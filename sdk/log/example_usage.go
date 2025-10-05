// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"time"
)

// ExampleUsage demonstrates how to use the audit log feature
func ExampleUsage() {
	fmt.Println("=== OpenTelemetry Go Audit Log Example ===")

	// 1. Create audit log store (file-based for persistence)
	store, err := NewAuditLogFileStore("/tmp/audit_logs")
	if err != nil {
		fmt.Printf("Failed to create audit log store: %v\n", err)
		return
	}
	fmt.Println("✅ Created file-based audit log store at /tmp/audit_logs")

	// 2. Create custom exporter
	exporter := &ExampleExporter{}
	fmt.Println("✅ Created custom exporter")

	// 3. Create audit exception handler
	exceptionHandler := &ExampleExceptionHandler{}
	fmt.Println("✅ Created custom exception handler")

	// 4. Create audit log processor with builder
	processor, err := NewAuditLogProcessorBuilder(exporter, store).
		SetScheduleDelay(2 * time.Second).    // Export every 2 seconds
		SetMaxExportBatchSize(5).             // Small batch for demo
		SetExporterTimeout(10 * time.Second). // 10 second timeout
		SetRetryPolicy(RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    time.Second,
			MaxBackoff:        5 * time.Second,
			BackoffMultiplier: 2.0,
		}).
		SetExceptionHandler(exceptionHandler).
		SetWaitOnExport(false).
		Build()

	if err != nil {
		fmt.Printf("Failed to create audit log processor: %v\n", err)
		return
	}
	fmt.Println("✅ Created audit log processor")

	// Ensure cleanup on exit
	defer func() {
		fmt.Println("\n=== Shutting down audit log processor ===")
		if err := processor.Shutdown(context.Background()); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		fmt.Println("✅ Audit log processor shutdown complete")
	}()

	// 5. Create logger using the audit processor
	logger := NewLogger(processor)
	fmt.Println("✅ Created logger with audit processor")

	// 6. Emit various audit log records
	fmt.Println("\n=== Emitting audit log records ===")

	ctx := context.Background()

	// Critical security event
	securityRecord := &Record{
		Timestamp: time.Now(),
		Severity:  SeverityError,
		Body:      StringValue("SECURITY: Unauthorized access attempt"),
	}
	securityRecord.AddAttributes(
		String("user_id", "hacker123"),
		String("ip_address", "192.168.1.100"),
		String("event_type", "security_breach"),
	)

	if err := logger.Emit(ctx, securityRecord); err != nil {
		fmt.Printf("❌ Failed to emit security record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted security audit record")
	}

	// User login event
	loginRecord := &Record{
		Timestamp: time.Now().Add(time.Millisecond),
		Severity:  SeverityInfo,
		Body:      StringValue("USER: Successful login"),
	}
	loginRecord.AddAttributes(
		String("user_id", "john.doe"),
		String("session_id", "sess_abc123"),
		String("login_method", "password"),
	)

	if err := logger.Emit(ctx, loginRecord); err != nil {
		fmt.Printf("❌ Failed to emit login record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted login audit record")
	}

	// Data access event
	dataRecord := &Record{
		Timestamp: time.Now().Add(2 * time.Millisecond),
		Severity:  SeverityWarn,
		Body:      StringValue("DATA: Sensitive data accessed"),
	}
	dataRecord.AddAttributes(
		String("user_id", "jane.smith"),
		String("data_type", "customer_pii"),
		String("operation", "read"),
		String("record_count", "150"),
	)

	if err := logger.Emit(ctx, dataRecord); err != nil {
		fmt.Printf("❌ Failed to emit data access record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted data access audit record")
	}

	fmt.Printf("\n=== Waiting for processing (queue size: %d) ===\n", processor.GetQueueSize())

	// Wait for processing to complete
	time.Sleep(5 * time.Second)

	fmt.Printf("Final queue size: %d\n", processor.GetQueueSize())
	fmt.Printf("Retry attempts: %d\n", processor.GetRetryAttempts())

	fmt.Println("\n=== Example completed successfully! ===")
}

// ExampleExporter is a simple exporter that writes logs to console
type ExampleExporter struct{}

func (e *ExampleExporter) Export(ctx context.Context, records []Record) error {
	fmt.Printf("=== AUDIT LOG EXPORT (%d records) ===\n", len(records))
	for i, record := range records {
		fmt.Printf("[%d] %s - %s: %s\n",
			i+1,
			record.Timestamp().Format("2006-01-02 15:04:05"),
			record.Severity().String(),
			record.Body().String())

		// Print attributes if any
		record.Attributes().Do(func(kv KeyValue) {
			fmt.Printf("    %s: %s\n", kv.Key, kv.Value.AsString())
		})
	}
	fmt.Println("=====================================")
	return nil
}

func (e *ExampleExporter) Shutdown(ctx context.Context) error {
	fmt.Println("ExampleExporter shutting down...")
	return nil
}

func (e *ExampleExporter) ForceFlush(ctx context.Context) error {
	fmt.Println("ExampleExporter flushing...")
	return nil
}

// ExampleExceptionHandler handles audit exceptions
type ExampleExceptionHandler struct {
	alertCount int
}

func (h *ExampleExceptionHandler) Handle(exception *AuditException) {
	h.alertCount++
	fmt.Printf("🚨 AUDIT ALERT #%d: %s\n", h.alertCount, exception.Message)
	if exception.Cause != nil {
		fmt.Printf("   Cause: %v\n", exception.Cause)
	}
	fmt.Printf("   Affected records: %d\n", len(exception.LogRecords))
}

