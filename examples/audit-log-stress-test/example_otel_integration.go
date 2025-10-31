// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func RunOTELIntegrationExample() {
	RunOTELIntegrationExampleWithEndpoint("http://localhost:4318")
}

func RunOTELIntegrationExampleWithEndpoint(endpoint string) error {
	fmt.Println("=== OpenTelemetry Audit Log Integration Example ===")
	fmt.Println()

	fmt.Println("Step 1: Create OTLP Exporter")
	fmt.Printf("   Connecting to: %s\n", endpoint)
	fmt.Println()

	otlpExporter, err := NewOTLPExporter(endpoint)
	if err != nil {
		fmt.Printf("❌ Failed to create OTLP exporter: %v\n", err)
		fmt.Println()
		fmt.Println("💡 Make sure your OTEL Collector is running and accessible.")
		return err
	}
	defer otlpExporter.Shutdown(context.Background())
	fmt.Println("✅ OTLP Exporter created")

	fmt.Println()
	fmt.Println("Step 2: Create Audit Log Store")
	store := sdklog.NewAuditLogInMemoryStore()
	fmt.Println("✅ In-memory store created")

	fmt.Println()
	fmt.Println("Step 3: Create Audit Log Processor with OTLP Exporter")
	processor, err := sdklog.NewAuditLogProcessorBuilder(otlpExporter, store).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		SetExporterTimeout(30 * time.Second).
		Build()
	if err != nil {
		fmt.Printf("❌ Failed to create processor: %v\n", err)
		return err
	}
	defer processor.Shutdown(context.Background())
	fmt.Println("✅ Audit processor created with OTLP exporter")

	fmt.Println()
	fmt.Println("Step 4: Emit audit log records")
	ctx := context.Background()

	auditEvents := []struct {
		severity    log.Severity
		message     string
		attributes  []log.KeyValue
		description string
	}{
		{
			severity: log.SeverityError,
			message:  "SECURITY: Unauthorized access attempt detected",
			attributes: []log.KeyValue{
				log.String("event.type", "security.unauthorized_access"),
				log.String("user.id", "unknown"),
				log.String("source.ip", "192.168.1.100"),
				log.String("resource", "/admin/users"),
				log.String("action", "READ"),
				log.Bool("blocked", true),
			},
			description: "Security incident",
		},
		{
			severity: log.SeverityWarn,
			message:  "DATA: Sensitive PII data accessed",
			attributes: []log.KeyValue{
				log.String("event.type", "data.pii_access"),
				log.String("user.id", "user123"),
				log.String("data.type", "customer_pii"),
				log.String("operation", "SELECT"),
				log.Int64("record.count", 150),
				log.String("table", "customers"),
			},
			description: "Data access audit",
		},
		{
			severity: log.SeverityInfo,
			message:  "AUTH: User login successful",
			attributes: []log.KeyValue{
				log.String("event.type", "auth.login_success"),
				log.String("user.id", "john.doe@example.com"),
				log.String("session.id", "sess_abc123xyz"),
				log.String("auth.method", "oauth2"),
				log.String("source.ip", "203.0.113.42"),
				log.String("user_agent", "Mozilla/5.0"),
			},
			description: "Authentication event",
		},
		{
			severity: log.SeverityWarn,
			message:  "COMPLIANCE: Retention policy violation",
			attributes: []log.KeyValue{
				log.String("event.type", "compliance.retention_violation"),
				log.String("policy.id", "gdpr-retention-2024"),
				log.String("data.category", "user_data"),
				log.Int64("days.overdue", 45),
				log.String("action.required", "archive_or_delete"),
			},
			description: "Compliance audit",
		},
		{
			severity: log.SeverityError,
			message:  "AUDIT: Failed to export financial transaction",
			attributes: []log.KeyValue{
				log.String("event.type", "audit.export_failure"),
				log.String("transaction.id", "txn-98765"),
				log.Float64("amount", 15000.50),
				log.String("currency", "USD"),
				log.String("error", "network_timeout"),
			},
			description: "Critical audit failure",
		},
	}

	for i, event := range auditEvents {
		record := sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetSeverity(event.severity)
		record.SetBody(log.StringValue(event.message))
		record.AddAttributes(event.attributes...)

		if err := processor.OnEmit(ctx, &record); err != nil {
			fmt.Printf("   ❌ [%d] Failed to emit %s: %v\n", i+1, event.description, err)
		} else {
			fmt.Printf("   ✅ [%d] %s\n", i+1, event.description)
			fmt.Printf("       %s - %s\n", event.severity, event.message)
		}
	}

	fmt.Println()
	fmt.Printf("   Total records queued: %d\n", processor.GetQueueSize())

	fmt.Println()
	fmt.Println("Step 5: Wait for export to OTEL Collector")
	fmt.Println("   Waiting for scheduled export...")
	time.Sleep(3 * time.Second)

	fmt.Println()
	fmt.Printf("   Queue size after export: %d\n", processor.GetQueueSize())
	fmt.Printf("   Retry attempts: %d\n", processor.GetRetryAttempts())

	fmt.Println()
	fmt.Println("Step 6: Force flush remaining logs")
	if err := processor.ForceFlush(context.Background()); err != nil {
		fmt.Printf("   ❌ Failed to flush: %v\n", err)
	} else {
		fmt.Println("   ✅ All logs flushed to OTEL")
	}

	fmt.Println()
	fmt.Println("✅ Integration example completed!")
	fmt.Println()
	fmt.Println("🔍 Check your OTEL Collector logs to see the exported audit logs")
	fmt.Println("   Or view them in your observability backend (Jaeger, Prometheus, etc.)")

	return nil
}

type OTLPExporter struct {
	endpoint   string
	logsURL    string
	httpClient *http.Client
}

func NewOTLPExporter(endpoint string) (*OTLPExporter, error) {
	fmt.Printf("   Connecting to OTLP endpoint: %s\n", endpoint)

	logsURL := endpoint
	if !strings.HasSuffix(logsURL, "/v1/logs") {
		if strings.HasSuffix(logsURL, "/") {
			logsURL = logsURL + "v1/logs"
		} else {
			logsURL = logsURL + "/v1/logs"
		}
	}

	return &OTLPExporter{
		endpoint: endpoint,
		logsURL:  logsURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (e *OTLPExporter) Export(ctx context.Context, records []sdklog.Record) error {
	if len(records) == 0 {
		return nil
	}

	fmt.Printf("\n📤 OTLP Export to %s (%d records)\n", e.logsURL, len(records))

	payload := e.buildOTLPPayload(records)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal OTLP payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.logsURL, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OTLP endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	for i, record := range records {
		fmt.Printf("   [%d] %s - %s\n",
			i+1,
			record.Severity().String(),
			record.Body().String())
	}

	fmt.Printf("   ✅ Successfully sent to OTEL Collector (status: %d)\n", resp.StatusCode)
	return nil
}

func (e *OTLPExporter) buildOTLPPayload(records []sdklog.Record) map[string]interface{} {
	logRecords := make([]map[string]interface{}, 0, len(records))

	for _, record := range records {
		attrs := make([]map[string]interface{}, 0)
		record.WalkAttributes(func(kv log.KeyValue) bool {
			attr := map[string]interface{}{
				"key": kv.Key,
			}

			switch kv.Value.Kind() {
			case log.KindString:
				attr["value"] = map[string]interface{}{"stringValue": kv.Value.AsString()}
			case log.KindInt64:
				attr["value"] = map[string]interface{}{"intValue": fmt.Sprintf("%d", kv.Value.AsInt64())}
			case log.KindFloat64:
				attr["value"] = map[string]interface{}{"doubleValue": kv.Value.AsFloat64()}
			case log.KindBool:
				attr["value"] = map[string]interface{}{"boolValue": kv.Value.AsBool()}
			default:
				attr["value"] = map[string]interface{}{"stringValue": kv.Value.String()}
			}

			attrs = append(attrs, attr)
			return true
		})

		logRecord := map[string]interface{}{
			"timeUnixNano":   fmt.Sprintf("%d", record.Timestamp().UnixNano()),
			"severityNumber": int(record.Severity()),
			"severityText":   record.Severity().String(),
			"body": map[string]interface{}{
				"stringValue": record.Body().String(),
			},
			"attributes": attrs,
		}

		logRecords = append(logRecords, logRecord)
	}

	return map[string]interface{}{
		"resourceLogs": []map[string]interface{}{
			{
				"resource": map[string]interface{}{
					"attributes": []map[string]interface{}{
						{
							"key": "service.name",
							"value": map[string]interface{}{
								"stringValue": "audit-log-example",
							},
						},
					},
				},
				"scopeLogs": []map[string]interface{}{
					{
						"scope": map[string]interface{}{
							"name":    "go.opentelemetry.io/otel/sdk/log",
							"version": "0.14.0",
						},
						"logRecords": logRecords,
					},
				},
			},
		},
	}
}

func (e *OTLPExporter) Shutdown(ctx context.Context) error {
	fmt.Println("\n🛑 OTLP Exporter shutting down...")
	return nil
}

func (e *OTLPExporter) ForceFlush(ctx context.Context) error {
	fmt.Println("🔄 OTLP Exporter flushing...")
	return nil
}
