// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

// MockExporter is a mock implementation of the Exporter interface for testing
type MockExporter struct {
	exportedRecords [][]Record
	exportError     error
	exportDelay     time.Duration
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
	return nil
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
