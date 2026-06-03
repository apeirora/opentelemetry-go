// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"

	"go.opentelemetry.io/otel/audit"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// ExportResult holds per-record delivery receipts from an audit export.
type ExportResult struct {
	Receipts []audit.AuditReceipt
}

// Exporter delivers audit records to an audit sink.
type Exporter interface {
	Export(ctx context.Context, records []Record) (ExportResult, error)
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
}

// LogExporter adapts a standard log Exporter for audit pipelines without sink receipts.
func LogExporter(exp sdklog.Exporter) Exporter {
	if exp == nil {
		return nil
	}
	return logExporterAdapter{exp: exp}
}

type logExporterAdapter struct {
	exp sdklog.Exporter
}

func (a logExporterAdapter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	if err := a.exp.Export(ctx, records); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Receipts: ReceiptsFromRecords(records)}, nil
}

// ReceiptsFromRecords builds sink receipts from exported records (local integrity hash).
func ReceiptsFromRecords(records []Record) []audit.AuditReceipt {
	return receiptsFromRecords(records)
}

func (a logExporterAdapter) Shutdown(ctx context.Context) error {
	return a.exp.Shutdown(ctx)
}

func (a logExporterAdapter) ForceFlush(ctx context.Context) error {
	return a.exp.ForceFlush(ctx)
}
