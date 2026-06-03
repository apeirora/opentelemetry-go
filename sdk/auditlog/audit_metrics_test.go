// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestAuditMetricsEmittedAndExported(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	orig := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(orig) })

	m := newAuditMetrics(mp)
	if m == nil {
		t.Fatal("expected audit metrics")
	}

	ctx := context.Background()
	m.recordEmitted(ctx, 2)
	m.recordExported(ctx, 1)
	m.recordDropped(ctx, 1)
	m.adjustQueueDepth(ctx, 3)
	m.adjustQueueDepth(ctx, -1)
	m.recordExportDuration(ctx, 25*time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	assertSum(t, rm, "audit.records.emitted", 2)
	assertSum(t, rm, "audit.records.exported", 1)
	assertSum(t, rm, "audit.records.dropped", 1)
	assertSum(t, rm, "audit.queue.depth", 2)
	assertHistogramCount(t, rm, "audit.export.duration", 1)
}

func assertSum(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s: expected int64 sum", name)
			}
			if sum.DataPoints[0].Value != want {
				t.Fatalf("%s: got %d want %d", name, sum.DataPoints[0].Value, want)
			}
			return
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, want uint64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("%s: expected int64 histogram", name)
			}
			if hist.DataPoints[0].Count != want {
				t.Fatalf("%s: got count %d want %d", name, hist.DataPoints[0].Count, want)
			}
			return
		}
	}
	t.Fatalf("metric %q not found", name)
}

func TestWarnAuditRecordTimestampSkew(t *testing.T) {
	now := time.Now().UTC()
	rec := minimalAuditRecordNoTarget()
	rec.SetTimestamp(now)
	rec.SetObservedTimestamp(now.Add(10 * time.Second))
	rec.RecordID = "skew-test"
	warnAuditRecordTimestampSkew(rec, time.Second)
}
