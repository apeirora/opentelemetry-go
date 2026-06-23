// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
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

func (m *MockExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	if m.exportDelay > 0 {
		time.Sleep(m.exportDelay)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	recordsCopy := make([]Record, len(records))
	for i := range records {
		recordsCopy[i] = records[i].Clone()
	}
	m.exportedRecords = append(m.exportedRecords, recordsCopy)

	if m.exportError != nil {
		return ExportResult{}, m.exportError
	}
	return ExportOK(records), nil
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
		for j := range batch {
			result[i][j] = batch[j].Clone()
		}
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

type walkOnlyStore struct {
	records      []Record
	getAllCalled atomic.Bool
	walkCalled   atomic.Bool
}

func (s *walkOnlyStore) Save(ctx context.Context, record *Record) error {
	return nil
}

func (s *walkOnlyStore) RemoveAll(ctx context.Context, records []Record) error {
	return nil
}

func (s *walkOnlyStore) GetAll(ctx context.Context) ([]Record, error) {
	s.getAllCalled.Store(true)
	return nil, fmt.Errorf("GetAll should not be called when WalkRecords is available")
}

func (s *walkOnlyStore) WalkRecords(ctx context.Context, fn func(Record) error) error {
	s.walkCalled.Store(true)
	for _, record := range s.records {
		if err := fn(record); err != nil {
			return err
		}
	}
	return nil
}

type replayBatchExporter struct {
	mutex       sync.Mutex
	exportCalls int
	maxBatch    int
	total       int
}

func (e *replayBatchExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.exportCalls++
	if len(records) > e.maxBatch {
		e.maxBatch = len(records)
	}
	e.total += len(records)
	return ExportOK(records), nil
}

func (e *replayBatchExporter) Shutdown(ctx context.Context) error {
	return nil
}

func (e *replayBatchExporter) ForceFlush(ctx context.Context) error {
	return nil
}

func (e *replayBatchExporter) Stats() (calls, maxBatch, total int) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.exportCalls, e.maxBatch, e.total
}

type peekOnlyStore struct {
	mutex       sync.Mutex
	records     []Record
	peekCalled  atomic.Bool
	getAllCalled atomic.Bool
}

func (s *peekOnlyStore) Save(ctx context.Context, record *Record) error {
	return nil
}

func (s *peekOnlyStore) RemoveAll(ctx context.Context, records []Record) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ids := make(map[string]bool, len(records))
	for _, record := range records {
		recordCopy := record
		id, err := getAuditRecordID(&recordCopy)
		if err == nil {
			ids[id] = true
		}
	}
	filtered := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		recordCopy := record
		id, err := getAuditRecordID(&recordCopy)
		if err != nil || !ids[id] {
			filtered = append(filtered, record)
		}
	}
	s.records = filtered
	return nil
}

func (s *peekOnlyStore) GetAll(ctx context.Context) ([]Record, error) {
	s.getAllCalled.Store(true)
	return nil, fmt.Errorf("GetAll should not be called when PeekBatch is available")
}

func (s *peekOnlyStore) PeekBatch(ctx context.Context, limit int) ([]Record, error) {
	s.peekCalled.Store(true)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if limit <= 0 {
		return nil, nil
	}
	n := limit
	if n > len(s.records) {
		n = len(s.records)
	}
	out := make([]Record, n)
	copy(out, s.records[:n])
	return out, nil
}

type countingInMemoryStore struct {
	inner       *AuditLogInMemoryStore
	removeCalls atomic.Int64
}

func newCountingInMemoryStore() *countingInMemoryStore {
	return &countingInMemoryStore{inner: NewAuditLogInMemoryStore()}
}

func (s *countingInMemoryStore) Save(ctx context.Context, record *Record) error {
	return s.inner.Save(ctx, record)
}

func (s *countingInMemoryStore) RemoveAll(ctx context.Context, records []Record) error {
	s.removeCalls.Add(1)
	return s.inner.RemoveAll(ctx, records)
}

func (s *countingInMemoryStore) GetAll(ctx context.Context) ([]Record, error) {
	return s.inner.GetAll(ctx)
}

func (s *countingInMemoryStore) GetRecordCount() int {
	return s.inner.GetRecordCount()
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

	t.Run("FIFO Queue Ordering", func(t *testing.T) {
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

		exportedBatches := exporter.GetExportedRecords()
		if len(exportedBatches) == 0 {
			t.Fatal("expected at least one export batch")
		}
		var order []string
		for _, batch := range exportedBatches {
			for _, record := range batch {
				order = append(order, record.Body().AsString())
			}
		}
		lowIdx, highIdx := -1, -1
		for i, body := range order {
			switch body {
			case "low severity":
				lowIdx = i
			case "high severity":
				highIdx = i
			}
		}
		if lowIdx < 0 || highIdx < 0 {
			t.Fatalf("expected both records exported, got order %v", order)
		}
		if lowIdx > highIdx {
			t.Fatalf("expected FIFO order (low before high), got order %v", order)
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
		exporter := &connectionFailureExporter{}
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

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
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		ctx := context.Background()

		record := createTestRecord("test message", log.SeverityInfo)
		if err := processor.OnEmit(ctx, &record); err != nil {
			t.Fatalf("Failed to emit record: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		if store.GetRecordCount() == 0 {
			t.Error("Expected connection failure to persist record for retry")
		}
	})

	t.Run("HTTP Export Error Rejects Emit", func(t *testing.T) {
		exporter := &httpResponseFailureExporter{}
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		config := AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   exceptionHandler,
			ScheduleDelay:      50 * time.Millisecond,
			MaxExportBatchSize: 1,
			ExporterTimeout:    time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
		}

		processor, err := NewAuditLogProcessor(config)
		if err != nil {
			t.Fatalf("Failed to create processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		record := createTestRecord("http-error", log.SeverityInfo)
		if err := processor.OnEmit(context.Background(), &record); err == nil {
			t.Fatal("Expected emit to fail for HTTP collector error")
		}
		if store.GetRecordCount() != 0 {
			t.Fatalf("Expected empty store for HTTP error, got %d", store.GetRecordCount())
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

	t.Run("Sync Export When Collector Healthy", func(t *testing.T) {
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
			WaitOnExport:       true,
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

		builder, err := NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
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
	})

	t.Run("Builder Configuration", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()
		exceptionHandler := NewMockExceptionHandler()

		builder, err := NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		builder = builder.
			SetExceptionHandler(exceptionHandler).
			SetScheduleDelay(500 * time.Millisecond).
			SetMaxExportBatchSize(100).
			SetExporterTimeout(60 * time.Second).
			SetWaitOnExport(true)

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
	})

	t.Run("Builder Validation", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		builder, err := NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		if err := builder.ValidateConfig(); err != nil {
			t.Errorf("Expected valid config, got error: %v", err)
		}
	})

	t.Run("Builder Build", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		builder, err := NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		processor, err := builder.Build()
		if err != nil {
			t.Fatalf("Failed to build processor: %v", err)
		}
		defer processor.Shutdown(context.Background())

		if processor == nil {
			t.Error("Expected processor to be created")
		}
	})

	t.Run("Builder errors on invalid inputs", func(t *testing.T) {
		exporter := NewMockExporter()
		store := NewAuditLogInMemoryStore()

		if _, err := NewAuditLogProcessorBuilder(nil, store); err == nil {
			t.Error("expected error with nil exporter")
		}
		if _, err := NewAuditLogProcessorBuilder(exporter, nil); err == nil {
			t.Error("expected error with nil store")
		}

		builder, err := NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		builder.SetScheduleDelay(-1)
		if _, err := builder.Build(); err == nil {
			t.Error("expected error with negative schedule delay")
		}

		builder, err = NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		builder.SetMaxExportBatchSize(0)
		if _, err := builder.Build(); err == nil {
			t.Error("expected error with zero batch size")
		}

		builder, err = NewAuditLogProcessorBuilder(exporter, store)
		if err != nil {
			t.Fatalf("NewAuditLogProcessorBuilder: %v", err)
		}
		builder.SetExporterTimeout(-1)
		if _, err := builder.Build(); err == nil {
			t.Error("expected error with negative timeout")
		}
	})
}

type failThenSucceedExporter struct {
	mu                sync.Mutex
	failuresRemaining int
	exported          []Record
}

type connectionFailureGatedExporter struct {
	mu           sync.Mutex
	allowSuccess atomic.Bool
	exported     []Record
}

func (e *connectionFailureGatedExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	if !e.allowSuccess.Load() {
		return ExportResult{}, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return ExportOK(records), nil
}

func (e *connectionFailureGatedExporter) Shutdown(ctx context.Context) error  { return nil }
func (e *connectionFailureGatedExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *connectionFailureGatedExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

type gatedExporter struct {
	mu           sync.Mutex
	allowSuccess atomic.Bool
	exported     []Record
}

func (e *gatedExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	if !e.allowSuccess.Load() {
		return ExportResult{}, fmt.Errorf("export blocked")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return ExportOK(records), nil
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

func (e *failThenSucceedExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failuresRemaining > 0 {
		e.failuresRemaining--
		return ExportResult{}, fmt.Errorf("transient export failure")
	}
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return ExportOK(records), nil
}

func (e *failThenSucceedExporter) Shutdown(ctx context.Context) error { return nil }
func (e *failThenSucceedExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *failThenSucceedExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

type failThenSucceedConnectionExporter struct {
	mu                sync.Mutex
	failuresRemaining int
	exported          []Record
}

func newFailThenSucceedConnectionExporter(failures int) *failThenSucceedConnectionExporter {
	return &failThenSucceedConnectionExporter{failuresRemaining: failures, exported: make([]Record, 0)}
}

func (e *failThenSucceedConnectionExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failuresRemaining > 0 {
		e.failuresRemaining--
		return ExportResult{}, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}
	copied := make([]Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return ExportOK(records), nil
}

func (e *failThenSucceedConnectionExporter) Shutdown(ctx context.Context) error  { return nil }
func (e *failThenSucceedConnectionExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *failThenSucceedConnectionExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

func TestAuditLogProcessorNoLossRetryRecovery(t *testing.T) {
	exporter := newFailThenSucceedConnectionExporter(3)
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
	exporter := &connectionFailureGatedExporter{}
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
		t.Fatalf("expected first shutdown to fail while collector is unreachable")
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

func TestAuditLogProcessorLoadExistingRecordsUsesWalkRecords(t *testing.T) {
	exporter := NewMockExporter()
	records := []Record{
		createTestRecord("walk-0", log.SeverityInfo),
		createTestRecord("walk-1", log.SeverityInfo),
		createTestRecord("walk-2", log.SeverityInfo),
	}
	store := &walkOnlyStore{records: records}
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
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		exported := 0
		for _, batch := range exporter.GetExportedRecords() {
			exported += len(batch)
		}
		if exported >= len(records) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !store.walkCalled.Load() {
		t.Fatalf("expected WalkRecords to be used")
	}
	if store.getAllCalled.Load() {
		t.Fatalf("GetAll should not be called when WalkRecords is available")
	}
}

func TestAuditLogProcessorLoadExistingRecordsStreamsBoundedBatches(t *testing.T) {
	exporter := &replayBatchExporter{}
	records := []Record{
		createTestRecord("stream-0", log.SeverityInfo),
		createTestRecord("stream-1", log.SeverityInfo),
		createTestRecord("stream-2", log.SeverityInfo),
		createTestRecord("stream-3", log.SeverityInfo),
		createTestRecord("stream-4", log.SeverityInfo),
	}
	store := &walkOnlyStore{records: records}
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
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, total := exporter.Stats()
		if total == len(records) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	calls, maxBatch, total := exporter.Stats()
	if total != len(records) {
		t.Fatalf("expected %d replayed records, got %d", len(records), total)
	}
	if maxBatch > cfg.MaxExportBatchSize {
		t.Fatalf("max replay batch %d exceeds configured %d", maxBatch, cfg.MaxExportBatchSize)
	}
	if calls < 3 {
		t.Fatalf("expected multiple replay batches, got %d calls", calls)
	}
	if processor.GetQueueSize() != 0 {
		t.Fatalf("expected replay queue to stay bounded, queue=%d", processor.GetQueueSize())
	}
}

func TestAuditLogProcessorLoadExistingRecordsUsesPeekBatch(t *testing.T) {
	exporter := &replayBatchExporter{}
	records := []Record{
		createTestRecord("peek-0", log.SeverityInfo),
		createTestRecord("peek-1", log.SeverityInfo),
		createTestRecord("peek-2", log.SeverityInfo),
	}
	store := &peekOnlyStore{records: records}
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
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, total := exporter.Stats()
		if total == len(records) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !store.peekCalled.Load() {
		t.Fatalf("expected PeekBatch to be used")
	}
	if store.getAllCalled.Load() {
		t.Fatalf("GetAll should not be called when PeekBatch is available")
	}
}

type connectionFailureExporter struct{}

func (e *connectionFailureExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	return ExportResult{}, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
}

func (e *connectionFailureExporter) Shutdown(ctx context.Context) error  { return nil }
func (e *connectionFailureExporter) ForceFlush(ctx context.Context) error { return nil }

type httpResponseFailureExporter struct{}

func (e *httpResponseFailureExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	return ExportResult{}, fmt.Errorf("failed to send logs to http://localhost:4318/v1/audit: 503 Service Unavailable (body: (empty))")
}

func (e *httpResponseFailureExporter) Shutdown(ctx context.Context) error  { return nil }
func (e *httpResponseFailureExporter) ForceFlush(ctx context.Context) error { return nil }

func TestAuditLogProcessorConnectionFailurePersistsForAsyncRetry(t *testing.T) {
	exporter := &connectionFailureExporter{}
	store := NewAuditLogInMemoryStore()
	handler := NewMockExceptionHandler()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   handler,
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	r := createTestRecord("conn-error-persist", log.SeverityInfo)
	if err := processor.OnEmit(context.Background(), &r); err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.GetRecordCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.GetRecordCount(); got == 0 {
		t.Fatalf("expected connection failure to be persisted for async retry, got empty store")
	}
}

func TestAuditLogProcessorHTTPFailureDoesNotPersist(t *testing.T) {
	exporter := &httpResponseFailureExporter{}
	store := NewAuditLogInMemoryStore()
	handler := NewMockExceptionHandler()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   handler,
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	r := createTestRecord("http-error-drop", log.SeverityInfo)
	if err := processor.OnEmit(context.Background(), &r); err == nil {
		t.Fatalf("expected emit to fail for HTTP collector error")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(handler.GetExceptions()) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.GetRecordCount(); got != 0 {
		t.Fatalf("expected empty store for HTTP failure, got %d", got)
	}
	exceptions := handler.GetExceptions()
	var found bool
	for _, ex := range exceptions {
		if ex.Message == "Collector returned an error; audit records are logged and not stored" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected collector error to be logged via exception handler, got %#v", exceptions)
	}
	if got := processor.GetQueueSize(); got != 0 {
		t.Fatalf("expected dropped HTTP failures to leave queue empty, got %d", got)
	}
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
	exporter := &connectionFailureGatedExporter{}
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
		t.Fatalf("expected shutdown error when collector is unreachable")
	}

	persisted, err := store.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed to load persisted records: %v", err)
	}
	if len(persisted) != recordsToEmit {
		t.Fatalf("expected %d persisted records after failed shutdown flush, got %d", recordsToEmit, len(persisted))
	}
}

func TestAuditLogProcessorMaxAttemptsStopsRequeue(t *testing.T) {
	exporter := &connectionFailureExporter{}
	store := NewAuditLogInMemoryStore()
	handler := NewMockExceptionHandler()
	cfg := AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   handler,
		ScheduleDelay:      time.Hour,
		MaxExportBatchSize: 8,
		ExporterTimeout:    time.Second,
		RetryPolicy: RetryPolicy{
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			MaxAttempts:       2,
		},
	}
	processor, err := NewAuditLogProcessor(cfg)
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	rec := createTestRecord("max-attempts", log.SeverityInfo)
	if err := processor.OnEmit(context.Background(), &rec); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	for i := 0; i < 6; i++ {
		_ = processor.ForceFlush(context.Background())
		if processor.GetRetryAttempts() >= 2 && processor.GetQueueSize() == 0 {
			break
		}
	}
	if processor.GetQueueSize() != 0 {
		t.Fatalf("expected queue drained after max attempts, size=%d", processor.GetQueueSize())
	}
	if processor.GetRetryAttempts() < 2 {
		t.Fatalf("expected at least 2 retry attempts, got %d", processor.GetRetryAttempts())
	}

	var maxMsg bool
	for _, ex := range handler.GetExceptions() {
		if strings.Contains(ex.Message, "after 2 retry attempts") {
			maxMsg = true
			break
		}
	}
	if !maxMsg {
		t.Fatalf("expected max-attempts exception, got %#v", handler.GetExceptions())
	}
}

func TestAuditLogProcessorQueueClonePreservesAttributes(t *testing.T) {
	exporter := NewMockExporter()
	store := NewAuditLogInMemoryStore()
	builder, err := NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := builder.
		SetScheduleDelay(50 * time.Millisecond).
		SetMaxExportBatchSize(1).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = processor.Shutdown(context.Background()) }()

	rec := Record{}
	now := time.Now().UTC()
	rec.SetTimestamp(now)
	rec.SetObservedTimestamp(now)
	rec.SetBody(log.StringValue(`{"k":1}`))
	rec.SetEventName("queue.clone.test")
	rec.AddAttributes(log.String("audit.record.id", "queue-clone-1"), log.String("audit.marker", "keep-me"))

	if err := processor.OnEmit(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	batches := exporter.GetExportedRecords()
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("expected one exported record, got %d batches", len(batches))
	}
	exported := batches[0][0]
	if got := exported.Body().String(); got != `{"k":1}` {
		t.Fatalf("body: got %q", got)
	}
	if exported.EventName() != "queue.clone.test" {
		t.Fatalf("event name: got %q", exported.EventName())
	}
	rec.SetBody(log.StringValue(`{"k":9}`))
	if got := exported.Body().String(); got != `{"k":1}` {
		t.Fatalf("exported body changed after mutating source: got %q", got)
	}
}
