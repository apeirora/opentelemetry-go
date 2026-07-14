// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlptracehttp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryAfterUsesSeconds(t *testing.T) {
	err := newResponseError(http.Header{"Retry-After": {"10"}}, nil)
	_, throttle := evaluate(err)
	assert.Equal(t, 10*time.Second, throttle)
}

func TestRetryAfterUsesHTTPDate(t *testing.T) {
	date := time.Now().UTC().Add(time.Hour).Format(http.TimeFormat)
	err := newResponseError(http.Header{"Retry-After": {date}}, nil)
	_, throttle := evaluate(err)
	assert.Greater(t, throttle, 59*time.Minute)
	assert.LessOrEqual(t, throttle, time.Hour)
}

func TestRetryAfterSecondsOverflow(t *testing.T) {
	err := newResponseError(http.Header{"Retry-After": {"9223372036854775807"}}, nil)
	_, throttle := evaluate(err)
	assert.Equal(t, time.Duration(1<<63-1), throttle)
}

func TestClientMarshalLogDoesNotIncludeEndpointConfig(t *testing.T) {
	const sensitiveEndpoint = "user:pass@collector.internal:4318"

	var buf bytes.Buffer
	logger := funcr.New(func(_, args string) {
		_, _ = buf.WriteString(args)
	}, funcr.Options{})

	client := NewClient(WithEndpoint(sensitiveEndpoint), WithInsecure())
	logger.Info("client", "config", client)

	logged := buf.String()
	assert.Contains(t, logged, "otlptracehttp")
	assert.NotContains(t, logged, sensitiveEndpoint)
	assert.NotContains(t, logged, "Insecure")
}

func TestUploadTracesFailoverInternal(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fallback.Close)

	fallbackHost := strings.TrimPrefix(fallback.URL, "http://")
	deadEndpoint := fmt.Sprintf("localhost:%s", portStr)

	c := NewClient(
		WithEndpoint(deadEndpoint),
		WithFallbackEndpoint(fallbackHost),
		WithInsecure(),
		WithRetry(RetryConfig{Enabled: false}),
	).(*client)

	ctx := context.Background()
	err = c.UploadTraces(ctx, nil)
	assert.NoError(t, err)
}
