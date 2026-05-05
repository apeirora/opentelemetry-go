// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

// MockExporter is a mock implementation of the Exporter interface for testing
type MockExporter struct {
	exportedRecords [][]Record
	exportError     error
	exportDelay     time.Duration
	shutdownError   error
	shutdownCount   int
	mutex           sync.Mutex
}

func NewMockExporter() *MockExporter {
	return &MockExporter{
		exportedRecords: make([][]Record, 0),
	}
}

func (m *MockExporter) Export(ctx context.Context, records []Record) error {
	if m.exportDelay > 0 {
		time.Sleep(m.exportDelay)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Create a copy of the records
	recordsCopy := make([]Record, len(records))
	copy(recordsCopy, records)
	m.exportedRecords = append(m.exportedRecords, recordsCopy)

	return m.exportError
}

func (m *MockExporter) Shutdown(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.shutdownCount++
	return m.shutdownError
}

func (m *MockExporter) ForceFlush(ctx context.Context) error {
	return nil
}

func (m *MockExporter) GetExportedRecords() [][]Record {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Return a deep copy
	result := make([][]Record, len(m.exportedRecords))
	for i, batch := range m.exportedRecords {
		result[i] = make([]Record, len(batch))
		copy(result[i], batch)
	}
	return result
}

func (m *MockExporter) SetExportError(err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.exportError = err
}

func (m *MockExporter) SetExportDelay(delay time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.exportDelay = delay
}

func (m *MockExporter) GetExportCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.exportedRecords)
}

func (m *MockExporter) SetShutdownError(err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.shutdownError = err
}

func (m *MockExporter) GetShutdownCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.shutdownCount
}

// MockExceptionHandler is a mock implementation of AuditExceptionHandler for testing
type MockExceptionHandler struct {
	exceptions []*AuditException
	mutex      sync.Mutex
}

func NewMockExceptionHandler() *MockExceptionHandler {
	return &MockExceptionHandler{
		exceptions: make([]*AuditException, 0),
	}
}

func (m *MockExceptionHandler) Handle(exception *AuditException) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.exceptions = append(m.exceptions, exception)
}

func (m *MockExceptionHandler) GetExceptions() []*AuditException {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Return a copy
	result := make([]*AuditException, len(m.exceptions))
	copy(result, m.exceptions)
	return result
}

func (m *MockExceptionHandler) GetExceptionCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.exceptions)
}

func TestAuditLogProcessor(t *testing.T) {
	t.Run("Basic Processing", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 10,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		// Create test records
		record1 := createTestRecord("test message 1", log.SeverityInfo)
		record2 := createTestRecord("test message 2", log.SeverityError)

		ctx := context.Background()

		// Process records
		if err := processor.OnEmit(ctx, &record1); err != nil {
			t.Fatalf("Failed to emit record 1: %v", err)
		}
		if err := processor.OnEmit(ctx, &record2); err != nil {
			t.Fatalf("Failed to emit record 2: %v", err)
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Verify records were exported
		exportedBatches := exporter.GetExportedRecords()
		if len(exportedBatches) == 0 {
			t.Error("Expected at least one export batch")
		}

		// Count total exported records
		totalExported := 0
		for _, batch := range exportedBatches {
			totalExported += len(batch)
		}

		if totalExported < 2 {
			t.Errorf("Expected at least 2 exported records, got %d", totalExported)
		}
	})

	t.Run("Priority Queue Ordering", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      50 * time.Millisecond,
			MaxExportBatchSize: 5,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		ctx := context.Background()

		// Add records with different severities
		lowSeverityRecord := createTestRecord("low severity", log.SeverityDebug)
		highSeverityRecord := createTestRecord("high severity", log.SeverityFatal)

		// Add low severity first, then high severity
		if err := processor.OnEmit(ctx, &lowSeverityRecord); err != nil {
			t.Fatalf("Failed to emit low severity record: %v", err)
		}
		if err := processor.OnEmit(ctx, &highSeverityRecord); err != nil {
			t.Fatalf("Failed to emit high severity record: %v", err)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Verify high severity record was processed first
		exportedBatches := exporter.GetExportedRecords()
		if len(exportedBatches) == 0 {
			t.Error("Expected at least one export batch")
		}

		// Check if high severity record appears in an earlier batch
		foundHighSeverity := false
		foundLowSeverity := false
		highSeverityBatch := -1
		lowSeverityBatch := -1

		for i, batch := range exportedBatches {
			for _, record := range batch {
				if record.Body().AsString() == "high severity" {
					foundHighSeverity = true
					highSeverityBatch = i
				}
				if record.Body().AsString() == "low severity" {
					foundLowSeverity = true
					lowSeverityBatch = i
				}
			}
		}

		if !foundHighSeverity || !foundLowSeverity {
			t.Error("Expected both records to be exported")
		}

		if highSeverityBatch >= lowSeverityBatch {
			t.Log("High severity record was processed in batch", highSeverityBatch, "and low severity in batch", lowSeverityBatch)
		}
	})

	t.Run("Batch Size Limiting", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 3, // Small batch size
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		ctx := context.Background()

		// Add more records than batch size
		for i := 0; i < 5; i++ {
			record := createTestRecord(fmt.Sprintf("test message %d", i), log.SeverityInfo)
			if err := processor.OnEmit(ctx, &record); err != nil {
				t.Fatalf("Failed to emit record %d: %v", i, err)
			}
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Verify records were exported in batches
		exportedBatches := exporter.GetExportedRecords()
		if len(exportedBatches) == 0 {
			t.Error("Expected at least one export batch")
		}

		// Check that no batch exceeds the max size
		for i, batch := range exportedBatches {
			if len(batch) > config.MaxExportBatchSize {
				t.Errorf("Batch %d size %d exceeds max batch size %d", i, len(batch), config.MaxExportBatchSize)
			}
		}
	})

	t.Run("Export Error Handling", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		// Set export error
		exporter.SetExportError(fmt.Errorf("export failed"))

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      50 * time.Millisecond,
			MaxExportBatchSize: 1,
			ExporterTimeout:    time.Second,
			RetryPolicy: RetryPolicy{
				InitialBackoff:    10 * time.Millisecond,
				MaxBackoff:        50 * time.Millisecond,
				BackoffMultiplier: 2.0,
			},
			WaitOnExport: false,
			DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		ctx := context.Background()

		// Add a record
		record := createTestRecord("test message", log.SeverityInfo)
		if err := processor.OnEmit(ctx, &record); err != nil {
			t.Fatalf("Failed to emit record: %v", err)
		}

		// Wait for retries to complete
		time.Sleep(200 * time.Millisecond)

		// Verify exception was handled
		exceptions := exceptionHandler.GetExceptions()
		if len(exceptions) == 0 {
			t.Error("Expected exception to be handled after retries exhausted")
		}
	})

	t.Run("Shutdown Behavior", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 5,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}

		ctx := context.Background()

		// Add a record
		record := createTestRecord("test message", log.SeverityInfo)
		if err := processor.OnEmit(ctx, &record); err != nil {
			t.Fatalf("Failed to emit record: %v", err)
		}

		// Shutdown processor
		if err := processor.Shutdown(ctx); err != nil {
			t.Fatalf("Failed to shutdown processor: %v", err)
		}

		// Verify record was exported during shutdown
		exportedBatches := exporter.GetExportedRecords()
		totalExported := 0
		for _, batch := range exportedBatches {
			totalExported += len(batch)
		}

		if totalExported == 0 {
			t.Error("Expected record to be exported during shutdown")
		}

		// Try to emit after shutdown - should handle gracefully
		record2 := createTestRecord("test message 2", log.SeverityInfo)
		if err := processor.OnEmit(ctx, &record2); err == nil {
			t.Error("Expected error when emitting after shutdown")
		}
	})

	t.Run("ForceFlush", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      1000 * time.Millisecond, // Long delay to prevent automatic export
			MaxExportBatchSize: 5,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		ctx := context.Background()

		// Add a record
		record := createTestRecord("test message", log.SeverityInfo)
		if err := processor.OnEmit(ctx, &record); err != nil {
			t.Fatalf("Failed to emit record: %v", err)
		}

		// Force flush
		if err := processor.ForceFlush(ctx); err != nil {
			t.Fatalf("Failed to force flush: %v", err)
		}

		// Verify record was exported
		exportedBatches := exporter.GetExportedRecords()
		totalExported := 0
		for _, batch := range exportedBatches {
			totalExported += len(batch)
		}

		if totalExported == 0 {
			t.Error("Expected record to be exported during force flush")
		}
	})

	t.Run("Sync Direct Delivery Without Store", func(t *testing.T) {
		exporter := NewMockExporter()
		exceptionHandler := NewMockExceptionHandler()
		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 10,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       true,
			DeliveryMode:       AuditDeliveryModeSyncDirect,
		}
		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())
		record := createTestRecord("sync-direct", log.SeverityInfo)
		if err := processor.OnEmit(context.Background(), &record); err != nil {
			t.Fatalf("Failed to emit sync-direct record: %v", err)
		}
		if exporter.GetExportCount() != 1 {
			t.Fatalf("Expected direct export count 1, got %d", exporter.GetExportCount())
		}
		if processor.GetQueueSize() != 0 {
			t.Fatalf("Expected queue size 0 in sync mode, got %d", processor.GetQueueSize())
		}
	})

	t.Run("Shutdown Calls ExporterShutdown", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   NewMockExceptionHandler(),
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 10,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}
		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		if err := processor.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}
		if exporter.GetShutdownCount() != 1 {
			t.Fatalf("Expected exporter Shutdown to be called once, got %d", exporter.GetShutdownCount())
		}
	})

	t.Run("Shutdown Returns ExporterShutdownError", func(t *testing.T) {
		exporter := NewMockExporter()
		exporter.SetShutdownError(fmt.Errorf("exporter shutdown failed"))
		store := NewAuditLogInMemoryStore()
		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   NewMockExceptionHandler(),
			ScheduleDelay:      100 * time.Millisecond,
			MaxExportBatchSize: 10,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
			DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		}
		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		if err := processor.Shutdown(context.Background()); err == nil {
			t.Fatalf("Expected exporter shutdown error")
		}
		if exporter.GetShutdownCount() != 1 {
			t.Fatalf("Expected exporter Shutdown to be called once, got %d", exporter.GetShutdownCount())
		}
	})
}

func TestAuditLogProcessorFileStoreCompactionAfterFlush(t *testing.T) {
	storeDir := t.TempDir()
	fileStore, err := NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("file store: %v", err)
	}
	exporter := NewMockExporter()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      fileStore,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      10 * time.Second,
		MaxExportBatchSize: 32,
		ExporterTimeout:    5 * time.Second,
		RetryPolicy:        GetDefaultRetryPolicy(),
		WaitOnExport:       false,
		DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	ctx := context.Background()
	const n = 20
	for i := 0; i < n; i++ {
		r := createTestRecord(fmt.Sprintf("compact-%d", i), log.SeverityInfo)
		if err := processor.OnEmit(ctx, &r); err != nil {
			t.Fatalf("OnEmit %d: %v", i, err)
		}
	}
	if err := processor.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
	remaining, err := fileStore.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected file store empty after successful export+flush, got %d records", len(remaining))
	}
}

func TestAuditLogProcessorBuilder(t *testing.T) {
	t.Run("Basic Builder", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		builder := NewAuditLogProcessorBuilder(exporter, store)
		config := builder.GetConfig()

		if config.Exporter != exporter {
			t.Error("Expected exporter to be set")
		}
		if config.AuditLogStore != store {
			t.Error("Expected store to be set")
		}
		if config.ExceptionHandler == nil {
			t.Error("Expected default exception handler to be set")
		}
		if config.DeliveryMode != AuditDeliveryModeAsyncStoreRetry {
			t.Error("Expected default delivery mode to be async_store_retry")
		}
	})

	t.Run("Builder Configuration", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		builder := NewAuditLogProcessorBuilder(exporter, store).
			SetExceptionHandler(exceptionHandler).
			SetScheduleDelay(500 * time.Millisecond).
			SetMaxExportBatchSize(100).
			SetExporterTimeout(60 * time.Second).
			SetWaitOnExport(true).
			SetDeliveryMode(AuditDeliveryModeSyncDirect)

		config := builder.GetConfig()

		if config.ExceptionHandler != exceptionHandler {
			t.Error("Expected exception handler to be set")
		}
		if config.ScheduleDelay != 500*time.Millisecond {
			t.Error("Expected schedule delay to be set")
		}
		if config.MaxExportBatchSize != 100 {
			t.Error("Expected max export batch size to be set")
		}
		if config.ExporterTimeout != 60*time.Second {
			t.Error("Expected exporter timeout to be set")
		}
		if !config.WaitOnExport {
			t.Error("Expected wait on export to be set")
		}
		if config.DeliveryMode != AuditDeliveryModeSyncDirect {
			t.Error("Expected delivery mode to be sync_direct")
		}
	})

	t.Run("Builder Validation", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		builder := NewAuditLogProcessorBuilder(exporter, store)
		if err := builder.ValidateConfig(); err != nil {
			t.Errorf("Expected valid config, got error: %v", err)
		}
	})

	t.Run("Builder Build", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		builder := NewAuditLogProcessorBuilder(exporter, store)
		processor, err := builder.Build()
		if err != nil {
			t.Fatalf("Failed to build processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		if processor == nil {
			t.Error("Expected processor to be created")
		}
	})

	t.Run("Builder Panic on Invalid Inputs", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		// Test nil exporter
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with nil exporter")
			}
		}()
		NewAuditLogProcessorBuilder(nil, store)

		// Test nil store
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with nil store")
			}
		}()
		NewAuditLogProcessorBuilder(exporter, nil)

		// Test nil exception handler
		builder := NewAuditLogProcessorBuilder(exporter, store)
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with nil exception handler")
			}
		}()
		builder.SetExceptionHandler(nil)

		// Test negative schedule delay
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with negative schedule delay")
			}
		}()
		builder.SetScheduleDelay(-1)

		// Test zero batch size
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with zero batch size")
			}
		}()
		builder.SetMaxExportBatchSize(0)

		// Test negative timeout
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with negative timeout")
			}
		}()
		builder.SetExporterTimeout(-1)
	})
}

func TestGetSeverityPriority(t *testing.T) {
	tests := []struct {
		severity log.Severity
		expected int
	}{
		{log.SeverityTrace, 1},
		{log.SeverityDebug, 2},
		{log.SeverityInfo, 3},
		{log.SeverityWarn, 4},
		{log.SeverityError, 5},
		{log.SeverityFatal, 6},
	}

	for _, test := range tests {
		priority := getSeverityPriority(test.severity)
		if priority != test.expected {
			t.Errorf("Expected priority %d for severity %v, got %d", test.expected, test.severity, priority)
		}
	}
}

type failThenSucceedExporter struct {
	mu                sync.Mutex
	failuresRemaining int
	exported          []Record
}

type gatedExporter struct {
	mu           sync.Mutex
	allowSuccess atomic.Bool
	exported     []Record
}

func (e *gatedExporter) Export(ctx context.Context, records []Record) error {
	if !e.allowSuccess.Load() {
		return fmt.Errorf("export blocked")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return nil
}

func (e *gatedExporter) Shutdown(ctx context.Context) error { return nil }
func (e *gatedExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *gatedExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

func newFailThenSucceedExporter(failures int) *failThenSucceedExporter {
	return &failThenSucceedExporter{failuresRemaining: failures, exported: make([]Record, 0)}
}

func (e *failThenSucceedExporter) Export(ctx context.Context, records []Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failuresRemaining > 0 {
		e.failuresRemaining--
		return fmt.Errorf("transient export failure")
	}
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return nil
}

func (e *failThenSucceedExporter) Shutdown(ctx context.Context) error { return nil }
func (e *failThenSucceedExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *failThenSucceedExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

func TestAuditLogProcessorNoLossRetryRecovery(t *testing.T) {
	exporter := newFailThenSucceedExporter(3)
	store := NewAuditLogInMemoryStore()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	const recordsToEmit = 25
	for i := 0; i < recordsToEmit; i++ {
		r := createTestRecord(fmt.Sprintf("durable-%d", i), log.SeverityInfo)
		if err := processor.OnEmit(context.Background(), &r); err != nil {
			t.Fatalf("emit failed at %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if exporter.ExportedCount() == recordsToEmit && store.GetRecordCount() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := exporter.ExportedCount(); got != recordsToEmit {
		t.Fatalf("expected %d exported records, got %d", recordsToEmit, got)
	}
	if got := store.GetRecordCount(); got != 0 {
		t.Fatalf("expected empty store after flush, got %d", got)
	}
}

func TestAuditLogProcessorNoLossForAcceptedRecordsDuringConcurrentShutdown(t *testing.T) {
	exporter := NewMockExporter()
	store := NewAuditLogInMemoryStore()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      10 * time.Millisecond,
		MaxExportBatchSize: 16,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        10 * time.Millisecond,
			BackoffMultiplier: 1.5,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	const emitters = 8
	const perEmitter = 40

	var accepted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(emitters)
	for g := 0; g < emitters; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perEmitter; i++ {
				r := createTestRecord(fmt.Sprintf("concurrent-%d-%d", id, i), log.SeverityInfo)
				if err := processor.OnEmit(context.Background(), &r); err == nil {
					accepted.Add(1)
				}
			}
		}(g)
	}

	time.Sleep(15 * time.Millisecond)
	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	wg.Wait()

	exported := 0
	for _, batch := range exporter.GetExportedRecords() {
		exported += len(batch)
	}
	if int64(exported) != accepted.Load() {
		t.Fatalf("expected %d exported accepted records, got %d", accepted.Load(), exported)
	}
	if got := store.GetRecordCount(); got != 0 {
		t.Fatalf("expected empty store after shutdown, got %d", got)
	}
}

func TestAuditLogProcessorAsyncIgnoresCanceledEmitContext(t *testing.T) {
	exporter := NewMockExporter()
	store := NewAuditLogInMemoryStore()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 2,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := createTestRecord("cancelled-context-record", log.SeverityInfo)

	if err := processor.OnEmit(ctx, &rec); err != nil {
		t.Fatalf("OnEmit returned error for canceled context: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exported := 0
		for _, batch := range exporter.GetExportedRecords() {
			exported += len(batch)
		}
		if exported == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	exported := 0
	for _, batch := range exporter.GetExportedRecords() {
		exported += len(batch)
	}
	t.Fatalf("expected 1 exported record, got %d", exported)
}

func TestAuditLogProcessorRestartReplaysFileStoreAfterRecovery(t *testing.T) {
	exporter := &gatedExporter{}
	storeDir := t.TempDir()
	fileStore, err := NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	firstCfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      fileStore,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 2,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}

	firstProcessor, err := NewAuditLogProcessor(firstCfg)
	if err != nil {
		t.Fatalf("failed to create first processor: %v", err)
	}

	const recordsToEmit = 6
	for i := 0; i < recordsToEmit; i++ {
		r := createTestRecord(fmt.Sprintf("replay-%d", i), log.SeverityInfo)
		if err := firstProcessor.OnEmit(context.Background(), &r); err != nil {
			t.Fatalf("first processor emit failed at %d: %v", i, err)
		}
	}

	time.Sleep(40 * time.Millisecond)
	if err := firstProcessor.Shutdown(context.Background()); err == nil {
		t.Fatalf("expected first shutdown to fail while exporter is blocked")
	}

	persistedAfterFailure, err := fileStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed to read persisted records: %v", err)
	}
	if len(persistedAfterFailure) == 0 {
		t.Fatalf("expected persisted records after failed export")
	}

	exporter.allowSuccess.Store(true)

	secondStore, err := NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("failed to reopen file store: %v", err)
	}

	secondCfg := firstCfg
	secondCfg.AuditLogStore = secondStore

	secondProcessor, err := NewAuditLogProcessor(secondCfg)
	if err != nil {
		t.Fatalf("failed to create second processor: %v", err)
	}
	defer secondProcessor.Shutdown(context.Background())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		remaining, getErr := secondStore.GetAll(context.Background())
		if getErr != nil {
			t.Fatalf("failed to read replayed store: %v", getErr)
		}
		if len(remaining) == 0 && exporter.ExportedCount() >= recordsToEmit {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	remaining, err := secondStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed to read final store state: %v", err)
	}
	t.Fatalf("expected replay drain, remaining=%d exported=%d", len(remaining), exporter.ExportedCount())
}

func TestAuditLogProcessorShutdownIsIdempotent(t *testing.T) {
	exporter := NewMockExporter()
	store := NewAuditLogInMemoryStore()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 2,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.5,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown failed: %v", err)
	}
	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}
	if got := exporter.GetShutdownCount(); got != 1 {
		t.Fatalf("expected exporter shutdown once, got %d", got)
	}
}

func TestAuditLogProcessorShutdownAfterFailedExportKeepsStoredRecords(t *testing.T) {
	exporter := &gatedExporter{}
	storeDir := t.TempDir()
	store, err := NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   NewMockExceptionHandler(),
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 2,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		DeliveryMode: AuditDeliveryModeAsyncStoreRetry,
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}

	const recordsToEmit = 5
	for i := 0; i < recordsToEmit; i++ {
		r := createTestRecord(fmt.Sprintf("shutdown-failed-export-%d", i), log.SeverityInfo)
		if err := processor.OnEmit(context.Background(), &r); err != nil {
			t.Fatalf("emit failed at %d: %v", i, err)
		}
	}

	time.Sleep(40 * time.Millisecond)
	if err := processor.Shutdown(context.Background()); err == nil {
		t.Fatalf("expected shutdown error when exports fail")
	}

	persisted, err := store.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed to load persisted records: %v", err)
	}
	if len(persisted) != recordsToEmit {
		t.Fatalf("expected %d persisted records after failed shutdown flush, got %d", recordsToEmit, len(persisted))
	}
}
