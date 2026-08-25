// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	m.recordDropped(ctx, 1, "retry_exhausted")
	m.recordRejected(ctx, 1, errTypeIntegrityVerify)
	m.recordStored(ctx, 3)
	m.recordStoreError(ctx, storeOpSave, errors.New("disk full"))
	m.adjustQueueDepth(ctx, 3)
	m.adjustQueueDepth(ctx, -1)
	m.recordExportDuration(ctx, 25*time.Millisecond, nil)
	m.recordExportDuration(ctx, 40*time.Millisecond, errors.New("dial timeout"))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	assertSum(t, rm, "audit.records.emitted", 2)
	assertSum(t, rm, "audit.records.exported", 1)
	assertSum(t, rm, "audit.records.dropped", 1)
	assertSum(t, rm, "audit.records.rejected", 1)
	assertSum(t, rm, "audit.records.stored", 3)
	assertSum(t, rm, "audit.store.errors", 1)
	assertSum(t, rm, "audit.queue.depth", 2)
	assertHistogramCount(t, rm, "audit.export.duration", 2)
	assertSumAttr(t, rm, "audit.records.rejected", attrErrorType, errTypeIntegrityVerify)
	assertSumAttr(t, rm, "audit.store.errors", attrStoreOperation, storeOpSave)
	assertHistogramHasErrorType(t, rm, "audit.export.duration")
}

func TestAuditMetricErrorTypeIntegrity(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{
			err:  newAuditStatusError(AuditErrorInvalidRequest, "audit integrity enrichment failed", false, errors.New("sign")),
			want: errTypeIntegrityEnrich,
		},
		{
			err:  newAuditStatusError(AuditErrorInvalidRequest, "audit.integrity.value verification failed", false, nil),
			want: errTypeIntegrityVerify,
		},
		{
			err:  newAuditStatusError(AuditErrorInvalidRequest, "audit integrity proof is required", false, nil),
			want: errTypeIntegrityRequire,
		},
		{
			err:  &AuditException{Status: AuditExceptionCollectorRejected, Message: "rejected"},
			want: string(AuditExceptionCollectorRejected),
		},
		{
			err:  newAuditStatusError(AuditErrorCollectorUnreachable, ReasonCollectorUnreachableStored, true, errors.New("dial")),
			want: string(AuditErrorCollectorUnreachable),
		},
	}
	for _, tc := range cases {
		if got := auditMetricErrorType(tc.err); got != tc.want {
			t.Fatalf("auditMetricErrorType(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}

func TestFinishEmitErrorSkipsStored(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m := newAuditMetrics(mp)
	if m == nil {
		t.Fatal("expected audit metrics")
	}
	_ = auditMetricsInstance()
	prev := packageMetrics
	packageMetrics = m
	t.Cleanup(func() { packageMetrics = prev })

	ctx := context.Background()
	stored := finishEmitError(ctx, AuditEmitResult{}, newAuditStatusError(
		AuditErrorCollectorUnreachable, ReasonCollectorUnreachableStored, true, errors.New("dial"),
	))
	if stored.Status != "stored" {
		t.Fatalf("status=%q want stored", stored.Status)
	}

	rejected := finishEmitError(ctx, AuditEmitResult{}, newAuditStatusError(
		AuditErrorInvalidRequest, "audit.integrity.value verification failed", false, nil,
	))
	if rejected.Status != "rejected" {
		t.Fatalf("status=%q want rejected", rejected.Status)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	assertSum(t, rm, "audit.records.rejected", 1)
	assertSumAttr(t, rm, "audit.records.rejected", attrErrorType, errTypeIntegrityVerify)
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
			var got int64
			for _, dp := range sum.DataPoints {
				got += dp.Value
			}
			if got != want {
				t.Fatalf("%s: got %d want %d", name, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertSumAttr(t *testing.T, rm metricdata.ResourceMetrics, name, key, want string) {
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
			for _, dp := range sum.DataPoints {
				v, ok := dp.Attributes.Value(attribute.Key(key))
				if ok && v.AsString() == want {
					return
				}
			}
			t.Fatalf("%s: missing attr %s=%q", name, key, want)
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
			var got uint64
			for _, dp := range hist.DataPoints {
				got += dp.Count
			}
			if got != want {
				t.Fatalf("%s: got count %d want %d", name, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertHistogramHasErrorType(t *testing.T, rm metricdata.ResourceMetrics, name string) {
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
			for _, dp := range hist.DataPoints {
				if _, ok := dp.Attributes.Value(attribute.Key(attrErrorType)); ok {
					return
				}
			}
			t.Fatalf("%s: expected a datapoint with %s", name, attrErrorType)
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
