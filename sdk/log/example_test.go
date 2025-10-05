// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAuditLogExample(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create audit log store
	store, err := NewAuditLogFileStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create audit log store: %v", err)
	}

	// Create mock exporter
	exporter := &MockExporter{}

	// Create processor
	processor, err := NewAuditLogProcessorBuilder(exporter, store).
		SetScheduleDelay(100 * time.Millisecond).
		SetMaxExportBatchSize(2).
		Build()
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	// Create logger
	logger := NewLogger(processor)

	// Emit some test records
	ctx := context.Background()

	// Test record 1
	record1 := &Record{
		Timestamp: time.Now(),
		Severity:  SeverityInfo,
		Body:      StringValue("Test audit message 1"),
	}
	record1.AddAttributes(String("test_key", "test_value"))

	if err := logger.Emit(ctx, record1); err != nil {
		t.Errorf("Failed to emit record 1: %v", err)
	}

	// Test record 2
	record2 := &Record{
		Timestamp: time.Now().Add(time.Millisecond),
		Severity:  SeverityWarn,
		Body:      StringValue("Test audit message 2"),
	}
	record2.AddAttributes(String("severity", "warning"))

	if err := logger.Emit(ctx, record2); err != nil {
		t.Errorf("Failed to emit record 2: %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify records were exported
	exportedRecords := exporter.GetExportedRecords()
	if len(exportedRecords) == 0 {
		t.Error("Expected records to be exported")
	}

	t.Logf("Successfully exported %d batches", len(exportedRecords))
	for i, batch := range exportedRecords {
		t.Logf("Batch %d: %d records", i+1, len(batch))
		for j, record := range batch {
			t.Logf("  Record %d: %s - %s", j+1, record.Severity().String(), record.Body().String())
		}
	}
}

func TestAuditLogFileStorage(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create file store
	store, err := NewAuditLogFileStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file store: %v", err)
	}

	// Test saving a record
	ctx := context.Background()
	record := &Record{
		Timestamp: time.Now(),
		Severity:  SeverityInfo,
		Body:      StringValue("Test persistence"),
	}
	record.AddAttributes(String("persistent", "true"))

	if err := store.Save(ctx, record); err != nil {
		t.Errorf("Failed to save record: %v", err)
	}

	// Test retrieving all records
	records, err := store.GetAll(ctx)
	if err != nil {
		t.Errorf("Failed to get all records: %v", err)
	}

	if len(records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(records))
	}

	if records[0].Body().String() != "Test persistence" {
		t.Errorf("Expected 'Test persistence', got '%s'", records[0].Body().String())
	}

	// Verify file exists
	logFile := tempDir + "/audit.log"
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("Expected log file to exist at %s", logFile)
	}

	t.Logf("Successfully tested file storage at %s", tempDir)
}
