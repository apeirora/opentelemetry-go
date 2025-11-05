// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package main

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== Simple Storage API Examples ===\n")

	memoryExample(ctx)
	fmt.Println()
	redisExample(ctx)
	fmt.Println()
	fileExample(ctx)
	fmt.Println()
	sqlExample(ctx)
}

func memoryExample(ctx context.Context) {
	fmt.Println("Example 1: Memory Storage (simplest)")
	fmt.Println("=====================================")

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
		WithMemoryStorage().
		SetScheduleDelay(100 * time.Millisecond).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := createTestRecord(1)
	processor.OnEmit(ctx, record)
	processor.ForceFlush(ctx)

	fmt.Println("✅ Memory storage - no configuration needed!")
	fmt.Println("   Use case: Testing, development")
}

func redisExample(ctx context.Context) {
	fmt.Println("Example 2: Redis Storage")
	fmt.Println("=========================")

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
		WithRedisStorage(
			sdklog.WithRedisEndpoint("localhost:6379"),
			sdklog.WithRedisKeyPrefix("myapp_"),
			sdklog.WithRedisKeyExpiration(24*time.Hour),
		).
		SetScheduleDelay(100 * time.Millisecond).
		Build()

	if err != nil {
		fmt.Printf("⚠️  Redis not available: %v\n", err)
		fmt.Println("   (Start Redis: docker run -d -p 6379:6379 redis:latest)")
		return
	}
	defer processor.Shutdown(ctx)

	record := createTestRecord(2)
	processor.OnEmit(ctx, record)
	processor.ForceFlush(ctx)

	fmt.Println("✅ Redis storage configured!")
	fmt.Println("   - Endpoint: localhost:6379")
	fmt.Println("   - Prefix: myapp_")
	fmt.Println("   - Expiration: 24h")
	fmt.Println("   Use case: Distributed systems, high availability")
}

func fileExample(ctx context.Context) {
	fmt.Println("Example 3: File Storage")
	fmt.Println("=======================")

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
		WithFileStorage("./example_storage").
		SetScheduleDelay(100 * time.Millisecond).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := createTestRecord(3)
	processor.OnEmit(ctx, record)
	processor.ForceFlush(ctx)

	fmt.Println("✅ File storage configured!")
	fmt.Println("   - Directory: ./example_storage")
	fmt.Println("   Use case: Single-node production, persistence")
}

func sqlExample(ctx context.Context) {
	fmt.Println("Example 4: SQL Storage")
	fmt.Println("======================")

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
		WithSQLStorage(
			sdklog.WithSQLDriver("sqlite3"),
			sdklog.WithSQLDatasource(":memory:"),
			sdklog.WithSQLTable("audit_logs"),
		).
		SetScheduleDelay(100 * time.Millisecond).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := createTestRecord(4)
	processor.OnEmit(ctx, record)
	processor.ForceFlush(ctx)

	fmt.Println("✅ SQL storage configured!")
	fmt.Println("   - Driver: sqlite3")
	fmt.Println("   - Database: :memory:")
	fmt.Println("   Use case: Enterprise, compliance, SQL queries")
}

func createTestRecord(id int) *sdklog.Record {
	record := &sdklog.Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(log.StringValue(fmt.Sprintf("Example audit log #%d", id)))
	record.AddAttributes(
		log.String("user_id", "user123"),
		log.String("action", "example_action"),
		log.Int64("example_id", int64(id)),
	)
	return record
}

type ConsoleExporter struct{}

func (e *ConsoleExporter) Export(ctx context.Context, records []sdklog.Record) error {
	for _, r := range records {
		fmt.Printf("   [EXPORTED] %s: %s\n", r.Severity(), r.Body())
	}
	return nil
}

func (e *ConsoleExporter) Shutdown(ctx context.Context) error { return nil }
func (e *ConsoleExporter) ForceFlush(ctx context.Context) error { return nil }

