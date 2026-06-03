// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stresstest_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/sdk/auditlog/otlpexport"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"

	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	"go.opentelemetry.io/otel/sdk/auditlog/stresstest/mockreceiver"
)

const stressHMACKey = "stresstest-hmac-key"

type countingExceptionHandler struct {
	mu         sync.Mutex
	exceptions []*auditlog.AuditException
}

func (h *countingExceptionHandler) Handle(exception *auditlog.AuditException) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.exceptions = append(h.exceptions, exception)
}

func (h *countingExceptionHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.exceptions)
}

func (h *countingExceptionHandler) lastMessage() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.exceptions) == 0 {
		return ""
	}
	return h.exceptions[len(h.exceptions)-1].Message
}

func (h *countingExceptionHandler) hasMessage(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.exceptions {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

type harnessOpts struct {
	receiverCfg      mockreceiver.Config
	maxBatchSize     int
	retryPolicy      auditlog.RetryPolicy
	deliveryMode     auditlog.AuditDeliveryMode
	storageWriteMode auditlog.AuditStorageWriteMode
	waitOnExport     bool
	fileStoreDir     string
	scheduleDelay    time.Duration
	exporterTimeout  time.Duration
}

type stressHarness struct {
	recv      *mockreceiver.Receiver
	exporter  auditlog.Exporter
	store     auditlog.AuditLogStore
	processor *auditlog.AuditLogProcessor
	provider  *auditlog.AuditLoggerProvider
	logger    auditlog.AuditLogger
	exHandler *countingExceptionHandler
}

func (h *stressHarness) pending() int {
	all, err := h.store.GetAll(context.Background())
	if err != nil {
		return -1
	}
	return len(all)
}

func (h *stressHarness) shutdownProcessor(ctx context.Context) error {
	return h.processor.Shutdown(ctx)
}

func (h *stressHarness) shutdownProvider(ctx context.Context) error {
	return h.provider.Shutdown(ctx)
}

func stressRecordCount(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("AUDIT_STRESS_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("invalid AUDIT_STRESS_COUNT %q: %v", v, err)
		}
		return n
	}
	if testing.Short() {
		return 50
	}
	return 200000
}

func guaranteeRecordCount(t *testing.T) int {
	t.Helper()
	if testing.Short() {
		return 10
		
	}
	return 25
}

func makeStressRecord(i int) auditlog.AuditRecord {
	now := time.Now().UTC()
	rid := uuid.NewString()
	base := logtest.RecordFactory{
		Timestamp:                 now,
		ObservedTimestamp:         now,
		Severity:                  log.SeverityInfo,
		Body:                      log.StringValue(fmt.Sprintf(`{"n":%d}`, i)),
		AttributeValueLengthLimit: -1,
		AttributeCountLimit:       -1,
		Attributes: []log.KeyValue{
			log.String("audit.record.id", rid),
			log.String("base", "stresstest"),
		},
	}.NewRecord()
	return auditlog.AuditRecord{
		Record:        base,
		EventName:     "stress.emit",
		Actor:         log.StringValue("stress@example.com"),
		ActorType:     "user",
		Action:        "emit",
		Resource:      log.StringValue("/stress"),
		Outcome:       "success",
		RecordID:      rid,
		SchemaVersion: "1.0",
	}
}

func newStressHarness(t *testing.T, opts harnessOpts) *stressHarness {
	t.Helper()

	recv, err := mockreceiver.Start(opts.receiverCfg)
	if err != nil {
		t.Fatalf("mock receiver: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = recv.Close(ctx)
	})

	exporter, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(recv.HostPort()),
		otlpexport.WithInsecure(),
		otlpexport.WithURLPath(recv.URLPath()),
	)
	if err != nil {
		t.Fatalf("otlp exporter: %v", err)
	}

	var store auditlog.AuditLogStore
	if opts.fileStoreDir != "" {
		store, err = auditlog.NewAuditLogFileStore(opts.fileStoreDir)
	} else {
		store = auditlog.NewAuditLogInMemoryStore()
	}
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}

	exHandler := &countingExceptionHandler{}
	maxBatch := opts.maxBatchSize
	if maxBatch <= 0 {
		maxBatch = 64
	}
	scheduleDelay := opts.scheduleDelay
	if scheduleDelay <= 0 {
		scheduleDelay = 5 * time.Millisecond
	}
	exporterTimeout := opts.exporterTimeout
	if exporterTimeout <= 0 {
		exporterTimeout = 500 * time.Millisecond
	}
	deliveryMode := opts.deliveryMode
	if deliveryMode == "" {
		deliveryMode = auditlog.AuditDeliveryModeAsyncStoreRetry
	}
	storageMode := opts.storageWriteMode
	if storageMode == "" {
		storageMode = auditlog.AuditStorageWriteAlways
	}
	retry := opts.retryPolicy
	if retry.InitialBackoff == 0 && retry.MaxBackoff == 0 && retry.BackoffMultiplier == 0 {
		retry = auditlog.RetryPolicy{
			InitialBackoff:    5 * time.Millisecond,
			MaxBackoff:        50 * time.Millisecond,
			BackoffMultiplier: 1.5,
		}
	}

	builder, err := auditlog.NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		t.Fatalf("processor builder: %v", err)
	}
	processor, err := builder.
		SetExceptionHandler(exHandler).
		SetDeliveryMode(deliveryMode).
		SetStorageWriteMode(storageMode).
		SetScheduleDelay(scheduleDelay).
		SetMaxExportBatchSize(maxBatch).
		SetExporterTimeout(exporterTimeout).
		SetRetryPolicy(retry).
		SetWaitOnExport(opts.waitOnExport).
		Build()
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = processor.Shutdown(ctx)
	})

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey([]byte(stressHMACKey)),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("stresstest")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	})

	return &stressHarness{
		recv:      recv,
		exporter:  exporter,
		store:     store,
		processor: processor,
		logger:    logger,
		provider:  provider,
		exHandler: exHandler,
	}
}

func newProcessorOnStore(
	t *testing.T,
	recv *mockreceiver.Receiver,
	store auditlog.AuditLogStore,
	opts harnessOpts,
) (*auditlog.AuditLogProcessor, *auditlog.AuditLoggerProvider, auditlog.AuditLogger, *countingExceptionHandler) {
	t.Helper()

	exporter, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(recv.HostPort()),
		otlpexport.WithInsecure(),
		otlpexport.WithURLPath(recv.URLPath()),
	)
	if err != nil {
		t.Fatalf("otlp exporter: %v", err)
	}

	exHandler := &countingExceptionHandler{}
	maxBatch := opts.maxBatchSize
	if maxBatch <= 0 {
		maxBatch = 1
	}
	scheduleDelay := opts.scheduleDelay
	if scheduleDelay <= 0 {
		scheduleDelay = 5 * time.Millisecond
	}
	exporterTimeout := opts.exporterTimeout
	if exporterTimeout <= 0 {
		exporterTimeout = 500 * time.Millisecond
	}
	retry := opts.retryPolicy
	if retry.InitialBackoff == 0 {
		retry = auditlog.RetryPolicy{
			InitialBackoff:    5 * time.Millisecond,
			MaxBackoff:        50 * time.Millisecond,
			BackoffMultiplier: 1.5,
		}
	}

	builder, err := auditlog.NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		t.Fatalf("processor builder: %v", err)
	}
	deliveryMode := opts.deliveryMode
	if deliveryMode == "" {
		deliveryMode = auditlog.AuditDeliveryModeAsyncStoreRetry
	}
	storageMode := opts.storageWriteMode
	if storageMode == "" {
		storageMode = auditlog.AuditStorageWriteAlways
	}

	processor, err := builder.
		SetExceptionHandler(exHandler).
		SetDeliveryMode(deliveryMode).
		SetStorageWriteMode(storageMode).
		SetScheduleDelay(scheduleDelay).
		SetMaxExportBatchSize(maxBatch).
		SetExporterTimeout(exporterTimeout).
		SetRetryPolicy(retry).
		SetWaitOnExport(opts.waitOnExport).
		Build()
	if err != nil {
		t.Fatalf("processor: %v", err)
	}

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey([]byte(stressHMACKey)),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("stresstest-restart")
	return processor, provider, logger, exHandler
}

func emitRecords(t *testing.T, logger auditlog.AuditLogger, total int) {
	t.Helper()
	for i := 0; i < total; i++ {
		rec := makeStressRecord(i)
		res := logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != 202 {
			t.Fatalf("emit %d: status=%d %s reason=%q", i, res.StatusCode, res.Status, res.Reason)
		}
	}
}

func emitRecordsExpectStatus(t *testing.T, logger auditlog.AuditLogger, total int, wantCode int) {
	t.Helper()
	for i := 0; i < total; i++ {
		rec := makeStressRecord(i)
		res := logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != wantCode {
			t.Fatalf("emit %d: want status %d got %d %s reason=%q", i, wantCode, res.StatusCode, res.Status, res.Reason)
		}
	}
}

func pendingStore(store auditlog.AuditLogStore) func() int {
	return func() int {
		all, err := store.GetAll(context.Background())
		if err != nil {
			return -1
		}
		return len(all)
	}
}
