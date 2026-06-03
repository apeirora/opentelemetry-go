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

// Option configures the audit OTLP/HTTP exporter.
type Option = otlploghttp.Option

var (
	WithEndpoint = otlploghttp.WithEndpoint
	WithInsecure = otlploghttp.WithInsecure
	WithURLPath  = otlploghttp.WithURLPath
	WithHeaders  = otlploghttp.WithHeaders
	WithTimeout  = otlploghttp.WithTimeout
)

// NewHTTP returns an auditlog.Exporter that sends OTLP audit traffic to POST /v1/audit.
func NewHTTP(ctx context.Context, opts ...Option) (auditlog.Exporter, error) {
	all := append([]Option{otlploghttp.WithURLPath(defaultAuditURLPath)}, opts...)
	inner, err := otlploghttp.New(ctx, all...)
	if err != nil {
		return nil, err
	}
	return &httpExporter{inner: inner}, nil
}

type httpExporter struct {
	inner *otlploghttp.Exporter
}

func (e *httpExporter) Export(ctx context.Context, records []auditlog.Record) (auditlog.ExportResult, error) {
	if err := e.inner.Export(ctx, records); err != nil {
		if msg := err.Error(); strings.Contains(msg, "partial") || strings.Contains(msg, "rejected") {
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
