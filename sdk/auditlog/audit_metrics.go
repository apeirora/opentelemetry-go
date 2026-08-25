// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk"
)

const auditMetricsScope = "go.opentelemetry.io/otel/sdk/auditlog"

const (
	attrErrorType           = "error.type"
	attrStoreOperation      = "audit.store.operation"
	storeOpSave             = "save"
	storeOpRemove           = "remove"
	errTypeIntegrityEnrich  = "integrity_enrichment_failed"
	errTypeIntegrityVerify  = "integrity_verification_failed"
	errTypeIntegrityRequire = "integrity_required"
	errTypeUnknown          = "unknown"
)

type auditMetrics struct {
	emitted        metric.Int64Counter
	exported       metric.Int64Counter
	dropped        metric.Int64Counter
	rejected       metric.Int64Counter
	stored         metric.Int64Counter
	storeErrors    metric.Int64Counter
	queueDepth     metric.Int64UpDownCounter
	exportDuration metric.Int64Histogram
}

var (
	auditMetricsOnce sync.Once
	packageMetrics   *auditMetrics
)

func auditMetricsInstance() *auditMetrics {
	auditMetricsOnce.Do(func() {
		packageMetrics = newAuditMetrics(otel.GetMeterProvider())
	})
	return packageMetrics
}

func newAuditMetrics(mp metric.MeterProvider) *auditMetrics {
	if mp == nil {
		return nil
	}
	meter := mp.Meter(
		auditMetricsScope,
		metric.WithInstrumentationVersion(sdk.Version()),
	)
	emitted, err := meter.Int64Counter(
		"audit.records.emitted",
		metric.WithDescription("Audit records accepted by the SDK pipeline"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	exported, err := meter.Int64Counter(
		"audit.records.exported",
		metric.WithDescription("Audit records successfully exported to the sink"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	dropped, err := meter.Int64Counter(
		"audit.records.dropped",
		metric.WithDescription("Audit records permanently discarded by the SDK"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	rejected, err := meter.Int64Counter(
		"audit.records.rejected",
		metric.WithDescription("Audit records rejected before durable accept or successful export"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	stored, err := meter.Int64Counter(
		"audit.records.stored",
		metric.WithDescription("Audit records persisted to the store after collector unreachable"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	storeErrors, err := meter.Int64Counter(
		"audit.store.errors",
		metric.WithDescription("Audit store operation failures"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return nil
	}
	queueDepth, err := meter.Int64UpDownCounter(
		"audit.queue.depth",
		metric.WithDescription("Audit export queue depth"),
		metric.WithUnit("{record}"),
	)
	if err != nil {
		return nil
	}
	exportDuration, err := meter.Int64Histogram(
		"audit.export.duration",
		metric.WithDescription("Audit export batch duration"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil
	}
	return &auditMetrics{
		emitted:        emitted,
		exported:       exported,
		dropped:        dropped,
		rejected:       rejected,
		stored:         stored,
		storeErrors:    storeErrors,
		queueDepth:     queueDepth,
		exportDuration: exportDuration,
	}
}

func (m *auditMetrics) recordEmitted(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.emitted.Add(ctx, n)
}

func (m *auditMetrics) recordExported(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.exported.Add(ctx, n)
}

func (m *auditMetrics) recordDropped(ctx context.Context, n int64, errorType string) {
	if m == nil || n <= 0 {
		return
	}
	m.dropped.Add(ctx, n, metric.WithAttributes(attribute.String(attrErrorType, normalizeErrorType(errorType))))
}

func (m *auditMetrics) recordRejected(ctx context.Context, n int64, errorType string) {
	if m == nil || n <= 0 {
		return
	}
	m.rejected.Add(ctx, n, metric.WithAttributes(attribute.String(attrErrorType, normalizeErrorType(errorType))))
}

func (m *auditMetrics) recordStored(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.stored.Add(ctx, n)
}

func (m *auditMetrics) recordStoreError(ctx context.Context, operation string, err error) {
	if m == nil || err == nil {
		return
	}
	m.storeErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrStoreOperation, operation),
		attribute.String(attrErrorType, auditMetricErrorType(err)),
	))
}

func (m *auditMetrics) adjustQueueDepth(ctx context.Context, delta int64) {
	if m == nil || delta == 0 {
		return
	}
	m.queueDepth.Add(ctx, delta)
}

func (m *auditMetrics) recordExportDuration(ctx context.Context, d time.Duration, err error) {
	if m == nil {
		return
	}
	if err == nil {
		m.exportDuration.Record(ctx, d.Milliseconds())
		return
	}
	m.exportDuration.Record(ctx, d.Milliseconds(), metric.WithAttributes(
		attribute.String(attrErrorType, auditMetricErrorType(err)),
	))
}

func normalizeErrorType(errorType string) string {
	errorType = strings.TrimSpace(errorType)
	if errorType == "" {
		return errTypeUnknown
	}
	return errorType
}

func auditMetricErrorType(err error) string {
	if err == nil {
		return errTypeUnknown
	}
	var ae *AuditException
	if errors.As(err, &ae) && ae.Status != "" {
		return string(ae.Status)
	}
	var se *AuditStatusError
	if errors.As(err, &se) {
		msg := se.Message
		switch {
		case strings.Contains(msg, "integrity enrichment"):
			return errTypeIntegrityEnrich
		case strings.Contains(msg, "integrity proof is required"):
			return errTypeIntegrityRequire
		case strings.Contains(msg, "integrity") || strings.Contains(msg, "audit.integrity"):
			return errTypeIntegrityVerify
		case msg == "provider_shutdown":
			return "provider_shutdown"
		case strings.Contains(msg, "processor_flush"):
			return "processor_flush_failed"
		case strings.Contains(msg, "audit_record_dropped"):
			return "audit_record_dropped"
		case msg == ReasonCollectorUnreachableStored:
			return string(AuditErrorCollectorUnreachable)
		default:
			if se.Code != "" {
				return string(se.Code)
			}
		}
	}
	return errTypeUnknown
}
