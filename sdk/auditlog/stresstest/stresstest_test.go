// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stresstest_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/auditlog/stresstest/mockreceiver"
)

func TestStressOTLPAllRecordsReceived(t *testing.T) {
	total := stressRecordCount(t)
	h := newStressHarness(t, harnessOpts{
		receiverCfg: mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: true},
		maxBatchSize: 64,
	})

	emitRecords(t, h.logger, total)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, total, h.pending); err != nil {
		t.Fatal(err)
	}
	if got := h.recv.UniqueRecordCount(); got != total {
		t.Fatalf("unique records: want %d got %d (accepted=%d requests=%d failed=%d)",
			total, got, h.recv.AcceptedRecords(), h.recv.RequestsTotal(), h.recv.FailedRequests())
	}
	if h.recv.AcceptedRecords() != uint64(total) {
		t.Fatalf("accepted record count: want %d got %d", total, h.recv.AcceptedRecords())
	}
	t.Logf("delivered %d records in %d HTTP requests (%d simulated failures)",
		total, h.recv.RequestsTotal(), h.recv.FailedRequests())
}

func TestStressOTLPWithIntermittentTimeouts(t *testing.T) {
	total := stressRecordCount(t)
	if total > 500 {
		total = 500
	}
	h := newStressHarness(t, harnessOpts{
		receiverCfg: mockreceiver.Config{
			URLPath:        "/v1/audit",
			StartAccepting: true,
			FailEveryN:     3,
			FailBehavior:   mockreceiver.FailBehaviorTimeout,
			FailDelay:      2 * time.Second,
		},
		maxBatchSize: 1,
	})

	emitRecords(t, h.logger, total)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, total, h.pending); err != nil {
		t.Fatal(err)
	}
	if got := h.recv.UniqueRecordCount(); got != total {
		t.Fatalf("unique records after retries: want %d got %d (accepted=%d requests=%d failed=%d pending=%d)",
			total, got, h.recv.AcceptedRecords(), h.recv.RequestsTotal(), h.recv.FailedRequests(), h.pending())
	}
	if h.recv.FailedRequests() == 0 {
		t.Fatal("expected some simulated timeout failures")
	}
	t.Logf("delivered %d records after %d requests (%d timeouts)",
		total, h.recv.RequestsTotal(), h.recv.FailedRequests())
}
