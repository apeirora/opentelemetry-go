// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlploggrpc

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	collogpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc/internal"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc/internal/failover"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc/internal/observ"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc/internal/retry"
)

// The methods of this type are not expected to be called concurrently.
type client struct {
	metadata         metadata.MD
	exportTimeout    time.Duration
	maxRequestSize   int
	requestFunc      retry.RequestFunc
	fallbackEndpoint string
	dialOpts         []grpc.DialOption

	ourConn bool
	conn    *grpc.ClientConn
	lsc     collogpb.LogsServiceClient

	ourFallbackConn bool
	fallbackConn    *grpc.ClientConn
	fallbackLsc     collogpb.LogsServiceClient

	instrumentation *observ.Instrumentation
}

// Used for testing.
var newGRPCClientFn = grpc.NewClient

// newClient creates a new gRPC log client.
func newClient(cfg config) (*client, error) {
	c := &client{
		exportTimeout:  cfg.timeout.Value,
		maxRequestSize: cfg.maxRequestSize.Value,
		requestFunc:    cfg.retryCfg.Value.RequestFunc(retryable),
		conn:           cfg.gRPCConn.Value,
	}
	if cfg.fallbackEndpoint.Set {
		c.fallbackEndpoint = cfg.fallbackEndpoint.Value
	}

	if len(cfg.headers.Value) > 0 {
		c.metadata = metadata.New(cfg.headers.Value)
	}

	c.dialOpts = newGRPCDialOptions(cfg)
	if c.conn == nil {
		conn, err := newGRPCClientFn(cfg.endpoint.Value, c.dialOpts...)
		if err != nil {
			return nil, err
		}
		// Keep track that we own the lifecycle of this conn and need to close
		// it on Shutdown.
		c.ourConn = true
		c.conn = conn
	}

	c.lsc = collogpb.NewLogsServiceClient(c.conn)

	var err error
	id := nextExporterID()
	c.instrumentation, err = observ.NewInstrumentation(id, c.conn.CanonicalTarget())
	return c, err
}

var exporterN atomic.Int64

// nextExporterID returns the next unique ID for an exporter.
func nextExporterID() int64 {
	const inc = 1
	return exporterN.Add(inc) - inc
}

func newGRPCDialOptions(cfg config) []grpc.DialOption {
	userAgent := "OTel Go OTLP over gRPC logs exporter/" + Version()
	dialOpts := []grpc.DialOption{grpc.WithUserAgent(userAgent)}
	dialOpts = append(dialOpts, cfg.dialOptions.Value...)

	// Convert other grpc configs to the dial options.
	// Service config
	if cfg.serviceConfig.Value != "" {
		dialOpts = append(dialOpts, grpc.WithDefaultServiceConfig(cfg.serviceConfig.Value))
	}
	// Prioritize GRPCCredentials over Insecure (passing both is an error).
	switch {
	case cfg.gRPCCredentials.Value != nil:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(cfg.gRPCCredentials.Value))
	case cfg.insecure.Value:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	default:
		// Default to using the host's root CA.
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(
			credentials.NewTLS(nil),
		))
	}
	// Compression
	if cfg.compression.Value == GzipCompression {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)))
	}
	// Reconnection period
	if cfg.reconnectionPeriod.Value != 0 {
		p := grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: cfg.reconnectionPeriod.Value,
		}
		dialOpts = append(dialOpts, grpc.WithConnectParams(p))
	}

	return dialOpts
}

// UploadLogs sends proto logs to connected endpoint.
//
// Retryable errors from the server will be handled according to any
// RetryConfig the client was created with.
//
// The otlplog.Exporter synchronizes access to client methods, and
// ensures this is not called after the Exporter is shutdown. Only thing
// to do here is send data.
func (c *client) UploadLogs(ctx context.Context, rl []*logpb.ResourceLogs) (uploadErr error) {
	select {
	case <-ctx.Done():
		// Do not upload if the context is already expired.
		return ctx.Err()
	default:
	}

	ctx, cancel := c.exportContext(ctx)
	defer cancel()

	pbRequest := &collogpb.ExportLogsServiceRequest{ResourceLogs: rl}
	if c.instrumentation != nil {
		var count int64
		for _, resLogs := range rl {
			for _, scopeLogs := range resLogs.ScopeLogs {
				count += int64(len(scopeLogs.LogRecords))
			}
		}
		eo := c.instrumentation.ExportLogs(ctx, count)
		defer func() {
			eo.End(uploadErr)
		}()
	}

	if maxSize := c.maxRequestSize; maxSize > 0 && proto.Size(pbRequest) > maxSize {
		return fmt.Errorf("request message too large: exceeded %d bytes", maxSize)
	}

	err := c.export(ctx, c.lsc, pbRequest, &uploadErr)
	if err == nil {
		return uploadErr
	}
	if c.fallbackEndpoint == "" || !failover.IsGRPCTransportError(err) {
		return err
	}
	if fbErr := c.ensureFallbackConn(); fbErr != nil {
		return errors.Join(fbErr, uploadErr)
	}
	return c.export(ctx, c.fallbackLsc, pbRequest, &uploadErr)
}

func (c *client) ensureFallbackConn() error {
	if c.fallbackLsc != nil {
		return nil
	}
	conn, err := newGRPCClientFn(c.fallbackEndpoint, c.dialOpts...)
	if err != nil {
		return err
	}
	c.ourFallbackConn = true
	c.fallbackConn = conn
	c.fallbackLsc = collogpb.NewLogsServiceClient(conn)
	return nil
}

func (c *client) export(
	ctx context.Context,
	lsc collogpb.LogsServiceClient,
	pbRequest *collogpb.ExportLogsServiceRequest,
	uploadErr *error,
) error {
	return c.requestFunc(ctx, func(ctx context.Context) error {
		resp, err := lsc.Export(ctx, pbRequest)
		if resp != nil && resp.PartialSuccess != nil {
			msg := resp.PartialSuccess.GetErrorMessage()
			n := resp.PartialSuccess.GetRejectedLogRecords()
			if n != 0 || msg != "" {
				err := internal.LogPartialSuccessError(n, msg)
				*uploadErr = errors.Join(*uploadErr, err)
			}
		}
		if status.Code(err) == codes.OK {
			return *uploadErr
		}
		return errors.Join(*uploadErr, err)
	})
}

// Shutdown shuts down the client, freeing all resources.
//
// Any active connections to a remote endpoint are closed if they were created
// by the client. Any gRPC connection passed during creation using
// WithGRPCConn will not be closed. It is the caller's responsibility to
// handle cleanup of that resource.
//
// The otlplog.Exporter synchronizes access to client methods and
// ensures this is called only once. The only thing that needs to be done
// here is to release any computational resources the client holds.
func (c *client) Shutdown(ctx context.Context) error {
	c.metadata = nil
	c.requestFunc = nil
	c.lsc = nil

	// Release the connection if we created it.
	err := ctx.Err()
	if c.ourConn {
		closeErr := c.conn.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}
	if c.ourFallbackConn && c.fallbackConn != nil {
		closeErr := c.fallbackConn.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}
	c.conn = nil
	return err
}

// exportContext returns a copy of parent with an appropriate deadline and
// cancellation function based on the clients configured export timeout.
//
// It is the callers responsibility to cancel the returned context once its
// use is complete, via the parent or directly with the returned CancelFunc, to
// ensure all resources are correctly released.
func (c *client) exportContext(parent context.Context) (context.Context, context.CancelFunc) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	if c.exportTimeout > 0 {
		ctx, cancel = context.WithTimeoutCause(parent, c.exportTimeout, errors.New("exporter export timeout"))
	} else {
		ctx, cancel = context.WithCancel(parent) //nolint:gosec  // cancel is handled by caller.
	}

	if c.metadata.Len() > 0 {
		md := c.metadata
		if outMD, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(md, outMD)
		}

		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	return ctx, cancel
}

type noopClient struct{}

func newNoopClient() *noopClient {
	return &noopClient{}
}

func (*noopClient) UploadLogs(context.Context, []*logpb.ResourceLogs) error { return nil }

func (*noopClient) Shutdown(context.Context) error { return nil }

// retryable returns if err identifies a request that can be retried and a
// duration to wait for if an explicit throttle time is included in err.
func retryable(err error) (bool, time.Duration) {
	s := status.Convert(err)
	return retryableGRPCStatus(s)
}

func retryableGRPCStatus(s *status.Status) (bool, time.Duration) {
	switch s.Code() {
	// Follows the retryable error codes defined in
	// https://opentelemetry.io/docs/specs/otlp/#failures
	case codes.Canceled,
		codes.DeadlineExceeded,
		codes.Aborted,
		codes.OutOfRange,
		codes.Unavailable,
		codes.DataLoss:
		// Additionally, handle RetryInfo.
		_, d := throttleDelay(s)
		return true, d
	case codes.ResourceExhausted:
		// Retry only if the server signals that the recovery from resource exhaustion is possible.
		return throttleDelay(s)
	}

	// Not a retry-able error.
	return false, 0
}

// throttleDelay returns if the status is RetryInfo
// and the duration to wait for if an explicit throttle time is included.
func throttleDelay(s *status.Status) (bool, time.Duration) {
	for _, detail := range s.Details() {
		if t, ok := detail.(*errdetails.RetryInfo); ok {
			return true, t.RetryDelay.AsDuration()
		}
	}
	return false, 0
}
