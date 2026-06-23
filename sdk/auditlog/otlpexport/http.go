// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
)

const defaultAuditURLPath = "/v1/audit"

// NewHTTP returns an auditlog.Exporter that sends OTLP audit traffic to POST /v1/audit.
func NewHTTP(ctx context.Context, opts ...Option) (auditlog.Exporter, error) {
	cfg := &buildConfig{
		verify: verifySettings{startupVerify: true},
	}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	all := append([]otlploghttp.Option{otlploghttp.WithURLPath(defaultAuditURLPath)}, cfg.otlpOpts...)
	inner, err := otlploghttp.New(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &httpExporter{
		inner:  inner,
		verify: cfg.verify.resolved(),
	}, nil
}

type httpExporter struct {
	inner  *otlploghttp.Exporter
	verify verifySettings
}

var _ auditlog.StartupExporterVerifier = (*httpExporter)(nil)

func (e *httpExporter) VerifyStartup(ctx context.Context) error {
	return verifyTLSAtStartup(ctx, e.verify)
}

func (e *httpExporter) Export(ctx context.Context, records []auditlog.Record) (auditlog.ExportResult, error) {
	if err := e.inner.Export(ctx, records); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "OTLP partial success") || strings.Contains(strings.ToLower(msg), "partial_success") {
			return auditlog.ExportResult{}, fmt.Errorf("audit: export failed (partial_success not allowed): %w", err)
		}
		return auditlog.ExportResult{}, err
	}
	return auditlog.ExportResult{Receipts: auditlog.ReceiptsFromRecords(records)}, nil
}

func (e *httpExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

func (e *httpExporter) ForceFlush(ctx context.Context) error {
	return e.inner.ForceFlush(ctx)
}
