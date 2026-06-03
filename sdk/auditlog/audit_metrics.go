// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk"
)

const auditMetricsScope = "go.opentelemetry.io/otel/sdk/auditlog"

type auditMetrics struct {
	emitted        metric.Int64Counter
	exported       metric.Int64Counter
	dropped        metric.Int64Counter
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
		metric.WithDescription("Audit records dropped after retry budget exhaustion"),
		metric.WithUnit("{record}"),
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

func (m *auditMetrics) recordDropped(ctx context.Context, n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.dropped.Add(ctx, n)
}

func (m *auditMetrics) adjustQueueDepth(ctx context.Context, delta int64) {
	if m == nil || delta == 0 {
		return
	}
	m.queueDepth.Add(ctx, delta)
}

func (m *auditMetrics) recordExportDuration(ctx context.Context, d time.Duration) {
	if m == nil {
		return
	}
	m.exportDuration.Record(ctx, d.Milliseconds())
}
