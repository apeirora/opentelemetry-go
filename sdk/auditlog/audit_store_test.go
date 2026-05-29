// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
	"go.opentelemetry.io/otel/trace"
)

func TestAuditLogFileStore(t *testing.T) {
	t.Run("NewAuditLogFileStore", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		if store.LogFilePath() == "" {
			t.Error("Expected log file path to be set")
		}

		// Verify file was created
		if _, err := os.Stat(store.LogFilePath()); os.IsNotExist(err) {
			t.Error("Expected log file to be created")
		}
	})

	t.Run("Save and GetAll", func(t *testing.T) {
		tempDir := t.TempDir()
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

	t.Run("Restart Preserves Records And Supports Removal", func(t *testing.T) {
		tempDir := t.TempDir()
		storePath := filepath.Join(tempDir, "audit.log")
		firstStore, err := NewAuditLogFileStore(storePath)
		if err != nil {
			t.Fatalf("Failed to create first file store: %v", err)
		}

		record1 := createTestRecord("restart record 1", log.SeverityInfo)
		record2 := createTestRecord("restart record 2", log.SeverityWarn)
		ctx := context.Background()

		if err := firstStore.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := firstStore.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		secondStore, err := NewAuditLogFileStore(storePath)
		if err != nil {
			t.Fatalf("Failed to create second file store: %v", err)
		}

		loaded, err := secondStore.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to load records after restart: %v", err)
		}
		if len(loaded) != 2 {
			t.Fatalf("Expected 2 records after restart, got %d", len(loaded))
		}

		if err := secondStore.RemoveAll(ctx, []Record{record1}); err != nil {
			t.Fatalf("Failed to remove persisted record after restart: %v", err)
		}
		remaining, err := secondStore.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get remaining records: %v", err)
		}
		if len(remaining) != 1 {
			t.Fatalf("Expected 1 remaining record after remove, got %d", len(remaining))
		}
	})

	t.Run("Restart With Truncated Record Preserves Decodable Records", func(t *testing.T) {
		tempDir := t.TempDir()
		storePath := filepath.Join(tempDir, "audit.log")
		store, err := NewAuditLogFileStore(storePath)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		record1 := createTestRecord("truncated-safe-1", log.SeverityInfo)
		record2 := createTestRecord("truncated-safe-2", log.SeverityWarn)
		ctx := context.Background()

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		file, err := os.OpenFile(storePath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			t.Fatalf("Failed to open file for corruption append: %v", err)
		}
		if _, err := file.WriteString("{\"timestamp\":\"broken"); err != nil {
			file.Close()
			t.Fatalf("Failed to append truncated record: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Failed to close corrupted file: %v", err)
		}

		restartedStore, err := NewAuditLogFileStore(storePath)
		if err != nil {
			t.Fatalf("Failed to restart file store: %v", err)
		}

		records, err := restartedStore.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to read records after restart: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("Expected 2 decodable records after restart, got %d", len(records))
		}
	})

	t.Run("RemoveAll Compaction Rewrites Malformed Entries", func(t *testing.T) {
		tempDir := t.TempDir()
		storePath := filepath.Join(tempDir, "audit.log")
		store, err := NewAuditLogFileStore(storePath)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		record1 := createTestRecord("compact-valid-1", log.SeverityInfo)
		record2 := createTestRecord("compact-valid-2", log.SeverityError)
		ctx := context.Background()

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		file, err := os.OpenFile(storePath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			t.Fatalf("Failed to open file for malformed append: %v", err)
		}
		if _, err := file.WriteString("{malformed-entry}\n"); err != nil {
			file.Close()
			t.Fatalf("Failed to append malformed line: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Failed to close malformed file: %v", err)
		}

		if err := store.RemoveAll(ctx, []Record{record1}); err != nil {
			t.Fatalf("Failed to remove one record with malformed line present: %v", err)
		}

		remaining, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to read remaining records: %v", err)
		}
		if len(remaining) != 1 {
			t.Fatalf("Expected one remaining valid record, got %d", len(remaining))
		}

		raw, err := os.ReadFile(storePath)
		if err != nil {
			t.Fatalf("Failed to read compacted log file: %v", err)
		}
		if strings.Contains(string(raw), "{malformed-entry}") {
			t.Fatalf("Expected malformed entry to be removed during compaction rewrite")
		}
	})

	t.Run("RemoveAll", func(t *testing.T) {
		tempDir := t.TempDir()
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

	t.Run("RemoveAll With Unknown Record Must Not Delete Stored Records", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		record1 := createTestRecord("kept message 1", log.SeverityInfo)
		record2 := createTestRecord("kept message 2", log.SeverityError)
		unknownRecord := createTestRecord("not persisted", log.SeverityWarn)
		ctx := context.Background()

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		if err := store.RemoveAll(ctx, []Record{unknownRecord}); err != nil {
			t.Fatalf("RemoveAll returned error: %v", err)
		}

		remainingRecords, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get remaining records: %v", err)
		}

		if len(remainingRecords) != 2 {
			t.Fatalf("Expected 2 remaining records, got %d", len(remainingRecords))
		}
	})

	t.Run("RemoveAll Repeated Call Is Idempotent", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := NewAuditLogFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create file store: %v", err)
		}

		record1 := createTestRecord("must stay 1", log.SeverityInfo)
		record2 := createTestRecord("must stay 2", log.SeverityError)
		unknownRecord := createTestRecord("never persisted idempotent", log.SeverityWarn)
		ctx := context.Background()

		if err := store.Save(ctx, &record1); err != nil {
			t.Fatalf("Failed to save record 1: %v", err)
		}
		if err := store.Save(ctx, &record2); err != nil {
			t.Fatalf("Failed to save record 2: %v", err)
		}

		if err := store.RemoveAll(ctx, []Record{unknownRecord}); err != nil {
			t.Fatalf("First RemoveAll returned error: %v", err)
		}
		if err := store.RemoveAll(ctx, []Record{unknownRecord}); err != nil {
			t.Fatalf("Second RemoveAll returned error: %v", err)
		}

		remainingRecords, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("Failed to get remaining records: %v", err)
		}

		if len(remainingRecords) != 2 {
			t.Fatalf("Expected 2 remaining records, got %d", len(remainingRecords))
		}
	})

	t.Run("Duplicate Prevention", func(t *testing.T) {
		tempDir := t.TempDir()
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
		if store.LogFilePath() != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, store.LogFilePath())
		}

		// Test with file path
		tempFile := filepath.Join(t.TempDir(), "custom.log")
		store2, err := NewAuditLogFileStore(tempFile)
		if err != nil {
			t.Fatalf("Failed to create file store with file: %v", err)
		}

		if store2.LogFilePath() != tempFile {
			t.Errorf("Expected path %s, got %s", tempFile, store2.LogFilePath())
		}
	})

	t.Run("Nil Record Handling", func(t *testing.T) {
		tempDir := t.TempDir()
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
		tempDir := t.TempDir()
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
	now := time.Now()
	return logtest.RecordFactory{
		Timestamp:                 now,
		ObservedTimestamp:         now,
		Severity:                  severity,
		SeverityText:              severity.String(),
		Body:                      log.StringValue(message),
		TraceID:                   trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:                    trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags:                trace.FlagsSampled,
		AttributeValueLengthLimit: -1,
		AttributeCountLimit:       -1,
		Attributes: []log.KeyValue{
			log.String("audit.record.id", fmt.Sprintf("%s-%d", message, now.UnixNano())),
			log.String("audit.hmac", fmt.Sprintf("hmac-%s", message)),
		},
	}.NewRecord()
}
