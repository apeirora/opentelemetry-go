// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stresstest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	"go.opentelemetry.io/otel/sdk/auditlog/stresstest/mockreceiver"
)

func defaultAsyncOpts() harnessOpts {
	return harnessOpts{
		receiverCfg:  mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: true},
		maxBatchSize: 1,
		waitOnExport: false,
	}
}

func TestStressCrashRecoveryFileStoreOTLP(t *testing.T) {
	n := guaranteeRecordCount(t)
	storeDir := t.TempDir()

	recv, err := mockreceiver.Start(mockreceiver.Config{
		URLPath:        "/v1/audit",
		StartAccepting: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = recv.Close(ctx)
	}()

	opts := defaultAsyncOpts()
	opts.maxBatchSize = 1

	store1, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	p1, prov1, logger1, _ := newProcessorOnStore(t, recv, store1, opts)
	emitRecords(t, logger1, n)
	time.Sleep(80 * time.Millisecond)
	_ = p1.Shutdown(context.Background())
	_ = prov1.Shutdown(context.Background())

	checkStore, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := checkStore.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != n {
		t.Fatalf("persisted after crash: want %d got %d", n, len(stored))
	}

	recv.SetAccepting(true)
	store2, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	p2, prov2, _, _ := newProcessorOnStore(t, recv, store2, opts)
	defer func() {
		_ = p2.Shutdown(context.Background())
		_ = prov2.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, recv, n, pendingStore(store2)); err != nil {
		t.Fatal(err)
	}
}

func TestStressSinkDownThenUp(t *testing.T) {
	n := guaranteeRecordCount(t)
	h := newStressHarness(t, harnessOpts{
		receiverCfg: mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false},
		maxBatchSize: 1,
	})

	emitRecords(t, h.logger, n)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForStoreCount(ctx, h.pending, n); err != nil {
		t.Fatal(err)
	}
	if h.recv.UniqueRecordCount() != 0 {
		t.Fatalf("sink down: expected 0 at receiver, got %d", h.recv.UniqueRecordCount())
	}

	h.recv.SetAccepting(true)
	if err := mockreceiver.WaitForDrain(ctx, h.recv, n, h.pending); err != nil {
		t.Fatal(err)
	}
}

func TestStressMaxAttemptsLeavesRecordsInStore(t *testing.T) {
	n := 5
	h := newStressHarness(t, harnessOpts{
		receiverCfg: mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false},
		maxBatchSize: 1,
		retryPolicy: auditlog.RetryPolicy{
			InitialBackoff:    2 * time.Millisecond,
			MaxBackoff:        10 * time.Millisecond,
			BackoffMultiplier: 1.2,
			MaxAttempts:       2,
		},
	})

	emitRecords(t, h.logger, n)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if h.exHandler.hasMessage("retry attempts") && h.pending() == n {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !h.exHandler.hasMessage("retry attempts") {
		t.Fatalf("expected max-attempts exception, last=%q count=%d", h.exHandler.lastMessage(), h.exHandler.count())
	}
	if h.recv.UniqueRecordCount() != 0 {
		t.Fatalf("sink never accepted: want 0 unique, got %d", h.recv.UniqueRecordCount())
	}
	if got := h.pending(); got != n {
		t.Fatalf("records must remain in store after max attempts: want %d got %d", n, got)
	}
}

func TestStressNoDuplicateAcceptedUnderFailures(t *testing.T) {
	n := guaranteeRecordCount(t)
	if n > 80 {
		n = 80
	}
	h := newStressHarness(t, harnessOpts{
		receiverCfg: mockreceiver.Config{
			URLPath:        "/v1/audit",
			StartAccepting: true,
			FailEveryN:     2,
			FailBehavior:   mockreceiver.FailBehaviorHTTP503,
		},
		maxBatchSize: 1,
	})

	emitRecords(t, h.logger, n)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, n, h.pending); err != nil {
		t.Fatal(err)
	}
	if got := h.recv.UniqueRecordCount(); got != n {
		t.Fatalf("unique: want %d got %d", n, got)
	}
	if h.recv.AcceptedRecords() != uint64(n) {
		t.Fatalf("accepted total must equal unique: want %d got %d (failures=%d requests=%d)",
			n, h.recv.AcceptedRecords(), h.recv.FailedRequests(), h.recv.RequestsTotal())
	}
}

func TestStressRejectedRecordsNeverReachSink(t *testing.T) {
	h := newStressHarness(t, defaultAsyncOpts())

	valid := 8
	for i := 0; i < valid; i++ {
		rec := makeStressRecord(i)
		res := h.logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != 202 {
			t.Fatalf("valid emit %d: %d %s", i, res.StatusCode, res.Reason)
		}
	}
	rejectedIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		rec := makeStressRecord(valid + i)
		rejectedIDs = append(rejectedIDs, rec.RecordID)
		rec.HMAC = "0000000000000000000000000000000000000000000000000000000000000000"
		res := h.logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != 400 {
			t.Fatalf("invalid emit %d: want 400 got %d %s", i, res.StatusCode, res.Reason)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, valid, h.pending); err != nil {
		t.Fatal(err)
	}
	if got := h.recv.UniqueRecordCount(); got != valid {
		t.Fatalf("sink accepted only valid records: want %d got %d", valid, got)
	}
	for _, id := range rejectedIDs {
		if h.recv.HasRecordID(id) {
			t.Fatalf("rejected record_id %q was accepted by sink", id)
		}
	}
}

func TestStressWaitOnExportDeliveredWhenSinkUp(t *testing.T) {
	h := newStressHarness(t, harnessOpts{
		receiverCfg:  mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: true},
		maxBatchSize: 1,
		waitOnExport: true,
	})

	rec := makeStressRecord(0)
	res := h.logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 200 || res.Status != "delivered" {
		t.Fatalf("want 200 delivered, got %d %s %q", res.StatusCode, res.Status, res.Reason)
	}
	if h.recv.UniqueRecordCount() != 1 {
		t.Fatalf("want 1 at sink, got %d", h.recv.UniqueRecordCount())
	}
	if h.pending() != 0 {
		t.Fatalf("store should be empty after delivered, pending=%d", h.pending())
	}
}

func TestStressWaitOnExportFailsWhenSinkDown(t *testing.T) {
	h := newStressHarness(t, harnessOpts{
		receiverCfg:  mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false},
		maxBatchSize: 1,
		waitOnExport: true,
	})

	rec := makeStressRecord(0)
	res := h.logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode == 200 {
		t.Fatal("expected non-delivered status when sink is down")
	}
	if h.recv.UniqueRecordCount() != 0 {
		t.Fatalf("sink down: want 0 received, got %d", h.recv.UniqueRecordCount())
	}
}

func TestStressFIFOOrderAtSink(t *testing.T) {
	n := guaranteeRecordCount(t)
	h := newStressHarness(t, harnessOpts{
		receiverCfg:  mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: true},
		maxBatchSize: 1,
	})

	emitRecords(t, h.logger, n)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, n, h.pending); err != nil {
		t.Fatal(err)
	}
	order := h.recv.AcceptedSeqOrder()
	if len(order) != n {
		t.Fatalf("order length: want %d got %d", n, len(order))
	}
	for i := 0; i < n; i++ {
		if order[i] != i {
			t.Fatalf("FIFO violation at %d: got seq %d want %d (full=%v)", i, order[i], i, order)
		}
	}
}

func TestStressSyncDirectDoesNotReplayFileStore(t *testing.T) {
	n := 5
	storeDir := t.TempDir()

	recv, err := mockreceiver.Start(mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = recv.Close(ctx)
	}()

	asyncOpts := defaultAsyncOpts()
	store1, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	p1, prov1, logger1, _ := newProcessorOnStore(t, recv, store1, asyncOpts)
	emitRecords(t, logger1, n)
	time.Sleep(50 * time.Millisecond)
	_ = p1.Shutdown(context.Background())
	_ = prov1.Shutdown(context.Background())

	stored, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	all, err := stored.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != n {
		t.Fatalf("want %d persisted, got %d", n, len(all))
	}

	recv.SetAccepting(true)
	recv.ResetStats()

	syncOpts := defaultAsyncOpts()
	syncOpts.deliveryMode = auditlog.AuditDeliveryModeSyncDirect
	storeSync, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	pSync, provSync, _, _ := newProcessorOnStore(t, recv, storeSync, syncOpts)
	time.Sleep(200 * time.Millisecond)
	_ = pSync.Shutdown(context.Background())
	_ = provSync.Shutdown(context.Background())

	if got := recv.UniqueRecordCount(); got != 0 {
		t.Fatalf("sync_direct must not replay file store backlog: sink got %d", got)
	}

	storeAsync, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	p2, prov2, _, _ := newProcessorOnStore(t, recv, storeAsync, asyncOpts)
	defer func() {
		_ = p2.Shutdown(context.Background())
		_ = prov2.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, recv, n, pendingStore(storeAsync)); err != nil {
		t.Fatal(err)
	}
}

func TestStressShutdownDrainsQueue(t *testing.T) {
	n := guaranteeRecordCount(t)
	h := newStressHarness(t, harnessOpts{
		receiverCfg:   mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: true},
		maxBatchSize:  128,
		scheduleDelay: 10 * time.Second,
	})

	emitRecords(t, h.logger, n)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := h.shutdownProvider(ctx); err != nil {
		t.Fatalf("provider shutdown: %v", err)
	}
	if err := mockreceiver.WaitForUniqueRecords(ctx, h.recv, n); err != nil {
		t.Fatal(err)
	}
	if h.pending() != 0 {
		t.Fatalf("store should be empty after shutdown flush, pending=%d", h.pending())
	}
}

func TestStressStorageWriteAlwaysPersistsBeforeExport(t *testing.T) {
	h := newStressHarness(t, harnessOpts{
		receiverCfg:      mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false},
		maxBatchSize:     512,
		scheduleDelay:    time.Hour,
		storageWriteMode: auditlog.AuditStorageWriteAlways,
	})

	rec := makeStressRecord(0)
	res := h.logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("emit: %d %s", res.StatusCode, res.Reason)
	}
	if got := h.pending(); got != 1 {
		t.Fatalf("write always: want 1 in store before export, got %d", got)
	}
	if h.recv.UniqueRecordCount() != 0 {
		t.Fatalf("export should not have succeeded yet, sink=%d", h.recv.UniqueRecordCount())
	}
}

func TestStressStorageWriteOnErrorPersistsAfterFailure(t *testing.T) {
	h := newStressHarness(t, harnessOpts{
		receiverCfg:      mockreceiver.Config{URLPath: "/v1/audit", StartAccepting: false},
		maxBatchSize:     1,
		scheduleDelay:    5 * time.Millisecond,
		storageWriteMode: auditlog.AuditStorageWriteOnError,
	})

	rec := makeStressRecord(0)
	res := h.logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("emit: %d %s", res.StatusCode, res.Reason)
	}
	if got := h.pending(); got != 0 {
		t.Fatalf("write on error: store should be empty before export fails, got %d", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mockreceiver.WaitForStoreCount(ctx, h.pending, 1); err != nil {
		t.Fatal(err)
	}
}

func TestStressConcurrentEmit(t *testing.T) {
	n := guaranteeRecordCount(t)
	if n < 20 {
		n = 20
	}
	opts := defaultAsyncOpts()
	opts.maxBatchSize = 32
	h := newStressHarness(t, opts)

	const workers = 4
	var wg sync.WaitGroup
	var emitErr sync.Map
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			rec := makeStressRecord(seq)
			res := h.logger.EmitWithResult(context.Background(), rec)
			if res.StatusCode != 202 {
				emitErr.Store(seq, res.Reason)
			}
		}(i)
	}
	wg.Wait()
	emitErr.Range(func(key, value any) bool {
		t.Fatalf("concurrent emit %d failed: %v", key, value)
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := mockreceiver.WaitForDrain(ctx, h.recv, n, h.pending); err != nil {
		t.Fatal(err)
	}
}
