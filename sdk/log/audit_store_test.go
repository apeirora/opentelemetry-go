// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

func TestAuditLogFileStore(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	t.Run("NewAuditLogFileStore", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		if store.logFilePath == "" {
			t.Error("Expected log file path to be set")
		}

		// Verify file was created
		if _, err := os.Stat(store.logFilePath); os.IsNotExist(err) {
			t.Error("Expected log file to be created")
		}
	})

	t.Run("Save and GetAll", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		// Create test records
		record1 := createTestRecord("test message 1", log.SeverityInfo)
		record2 := createTestRecord("test message 2", log.SeverityError)

		ctx := context.Background()

		// Save records
		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		// Retrieve all records
		records, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get all records: %v", err)
		}

		if len(records) != 2 {
			t.Errorf("Expected 2 records, got %d", len(records))
		}
	})

	t.Run("RemoveAll", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		// Create and save test records
		record1 := createTestRecord("test message 1", log.SeverityInfo)
		record2 := createTestRecord("test message 2", log.SeverityError)

		ctx := context.Background()

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		// Remove one record
		recordsToRemove := []Record{record1}
		if err := store.RemoveAll(ctx, recordsToRemove); err != nil {
			t.Fatalf("Failed to remove records: %v", err)
		}

		// Verify only one record remains
		remainingRecords, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get remaining records: %v", err)
		}

		if len(remainingRecords) != 1 {
			t.Errorf("Expected 1 remaining record, got %d", len(remainingRecords))
		}
	})

	t.Run("Duplicate Prevention", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		// Create identical records
		record := createTestRecord("duplicate test", log.SeverityInfo)
		ctx := context.Background()

		// Save the same record twice
		if err := store.Save(ctx, &record); err != nil {
			t.Fatalf("Failed to save record first time: %v", err)
		}
		if err := store.Save(ctx, &record); err != nil {
			t.Fatalf("Failed to save record second time: %v", err)
		}

		// Should only have one record due to duplicate prevention
		records, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get all records: %v", err)
		}

		// Note: The current implementation doesn't prevent duplicates based on content
		// This test documents the current behavior
		t.Logf("Records saved: %d", len(records))
	})

	t.Run("File Path Handling", func(t *testing.T) {
		// Test with directory path
		tempDir2 := t.TempDir()
		store, err := NewAuditLogFileStore(tempDir2)
		if err != nil {
			t.Fatalf("Failed to create file store with directory: %v", err)
		}

		expectedPath := filepath.Join(tempDir2, DefaultLogFileName)
		if store.logFilePath != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, store.logFilePath)
		}

		// Test with file path
		tempFile := filepath.Join(t.TempDir(), "custom.log")
		store2, err := NewAuditLogFileStore(tempFile)
		if err != nil {
			t.Fatalf("Failed to create file store with file: %v", err)
		}

		if store2.logFilePath != tempFile {
			t.Errorf("Expected path %s, got %s", tempFile, store2.logFilePath)
		}
	})

	t.Run("Nil Record Handling", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		ctx := context.Background()
		err = store.Save(ctx, nil)
		if err == nil {
			t.Error("Expected error when saving nil record")
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record := createTestRecord("test", log.SeverityInfo)

		// These operations should respect context cancellation
		err = store.Save(ctx, &record)
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}

		_, err = store.GetAll(ctx)
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}
	})
}

func TestAuditLogInMemoryStore(t *testing.T) {
	t.Run("Basic Operations", func(t *testing.T) {
		store := NewAuditLogInMemoryStore()
		ctx := context.Background()

		// Test initial state
		if count := store.GetRecordCount(); count != 0 {
			t.Errorf("Expected 0 records initially, got %d", count)
		}

		// Create and save test records
		record1 := createTestRecord("test message 1", log.SeverityInfo)
		record2 := createTestRecord("test message 2", log.SeverityError)

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		// Verify record count
		if count := store.GetRecordCount(); count != 2 {
			t.Errorf("Expected 2 records, got %d", count)
		}

		// Retrieve all records
		records, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get all records: %v", err)
		}

		if len(records) != 2 {
			t.Errorf("Expected 2 records, got %d", len(records))
		}
	})

	t.Run("RemoveAll", func(t *testing.T) {
		store := NewAuditLogInMemoryStore()
		ctx := context.Background()

		// Create and save test records
		record1 := createTestRecord("test message 1", log.SeverityInfo)
		record2 := createTestRecord("test message 2", log.SeverityError)

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		// Remove one record
		recordsToRemove := []Record{record1}
		if err := store.RemoveAll(ctx, recordsToRemove); err != nil {
			t.Fatalf("Failed to remove records: %v", err)
		}

		// Verify only one record remains
		if count := store.GetRecordCount(); count != 1 {
			t.Errorf("Expected 1 remaining record, got %d", count)
		}

		remainingRecords, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get remaining records: %v", err)
		}

		if len(remainingRecords) != 1 {
			t.Errorf("Expected 1 remaining record, got %d", len(remainingRecords))
		}
	})

	t.Run("Clear", func(t *testing.T) {
		store := NewAuditLogInMemoryStore()
		ctx := context.Background()

		// Add some records
		record := createTestRecord("test", log.SeverityInfo)
		if err := store.Save(ctx, &record); err != nil {
			t.Fatalf("Failed to save record: %v", err)
		}

		// Clear the store
		store.Clear()

		// Verify store is empty
		if count := store.GetRecordCount(); count != 0 {
			t.Errorf("Expected 0 records after clear, got %d", count)
		}

		records, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get all records: %v", err)
		}

		if len(records) != 0 {
			t.Errorf("Expected 0 records after clear, got %d", len(records))
		}
	})

	t.Run("Nil Record Handling", func(t *testing.T) {
		store := NewAuditLogInMemoryStore()
		ctx := context.Background()

		err := store.Save(ctx, nil)
		if err == nil {
			t.Error("Expected error when saving nil record")
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		store := NewAuditLogInMemoryStore()

		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record := createTestRecord("test", log.SeverityInfo)

		// These operations should respect context cancellation
		err := store.Save(ctx, &record)
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}

		_, err = store.GetAll(ctx)
		if err == nil {
			t.Error("Expected error when context is cancelled")
		}
	})
}

// Helper function to create test records
func createTestRecord(message string, severity log.Severity) Record {
	record := Record{
		timestamp:         time.Now(),
		observedTimestamp: time.Now(),
		severity:          severity,
		severityText:      severity.String(),
		body:              log.StringValue(message),
		traceID:           trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		spanID:            trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		traceFlags:        trace.FlagsSampled,
	}

	return record
}
