// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/log"
)

func ExampleUsage() {
	fmt.Println("=== OpenTelemetry Go Audit Log Example ===")

	store, err := NewAuditLogFileStore("/tmp/audit_logs")
	if err != nil {
		fmt.Printf("Failed to create audit log store: %v\n", err)
		return
	}
	fmt.Println("✅ Created file-based audit log store at /tmp/audit_logs")

	exporter := &ExampleExporter{}
	fmt.Println("✅ Created custom exporter")

	exceptionHandler := &ExampleExceptionHandler{}
	fmt.Println("✅ Created custom exception handler")

	processor, err := NewAuditLogProcessorBuilder(exporter, store).
		SetScheduleDelay(2 * time.Second).
		SetMaxExportBatchSize(5).
		SetExporterTimeout(10 * time.Second).
		SetRetryPolicy(RetryPolicy{
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

	defer func() {
		fmt.Println("\n=== Shutting down audit log processor ===")
		if err := processor.Shutdown(context.Background()); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		fmt.Println("✅ Audit log processor shutdown complete")
	}()

	fmt.Println("\n=== Emitting audit log records ===")

	ctx := context.Background()

	securityRecord := Record{}
	securityRecord.SetTimestamp(time.Now())
	securityRecord.SetSeverity(log.SeverityError)
	securityRecord.SetBody(log.StringValue("SECURITY: Unauthorized access attempt"))
	securityRecord.AddAttributes(
		log.String("user_id", "hacker123"),
		log.String("ip_address", "192.168.1.100"),
		log.String("event_type", "security_breach"),
	)

	if err := processor.OnEmit(ctx, &securityRecord); err != nil {
		fmt.Printf("❌ Failed to emit security record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted security audit record")
	}

	loginRecord := Record{}
	loginRecord.SetTimestamp(time.Now().Add(time.Millisecond))
	loginRecord.SetSeverity(log.SeverityInfo)
	loginRecord.SetBody(log.StringValue("USER: Successful login"))
	loginRecord.AddAttributes(
		log.String("user_id", "john.doe"),
		log.String("session_id", "sess_abc123"),
		log.String("login_method", "password"),
	)

	if err := processor.OnEmit(ctx, &loginRecord); err != nil {
		fmt.Printf("❌ Failed to emit login record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted login audit record")
	}

	dataRecord := Record{}
	dataRecord.SetTimestamp(time.Now().Add(2 * time.Millisecond))
	dataRecord.SetSeverity(log.SeverityWarn)
	dataRecord.SetBody(log.StringValue("DATA: Sensitive data accessed"))
	dataRecord.AddAttributes(
		log.String("user_id", "jane.smith"),
		log.String("data_type", "customer_pii"),
		log.String("operation", "read"),
		log.String("record_count", "150"),
	)

	if err := processor.OnEmit(ctx, &dataRecord); err != nil {
		fmt.Printf("❌ Failed to emit data access record: %v\n", err)
	} else {
		fmt.Println("✅ Emitted data access audit record")
	}

	fmt.Printf("\n=== Waiting for processing (queue size: %d) ===\n", processor.GetQueueSize())

	time.Sleep(5 * time.Second)

	fmt.Printf("Final queue size: %d\n", processor.GetQueueSize())
	fmt.Printf("Retry attempts: %d\n", processor.GetRetryAttempts())

	fmt.Println("\n=== Example completed successfully! ===")
}

type ExampleExporter struct{}

func (e *ExampleExporter) Export(ctx context.Context, records []Record) error {
	fmt.Printf("=== AUDIT LOG EXPORT (%d records) ===\n", len(records))
	for i, record := range records {
		fmt.Printf("[%d] %s - %s: %s\n",
			i+1,
			record.Timestamp().Format("2006-01-02 15:04:05"),
			record.Severity().String(),
			record.Body().String())

		record.WalkAttributes(func(kv log.KeyValue) bool {
			fmt.Printf("    %s: %s\n", kv.Key, kv.Value.String())
			return true
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
