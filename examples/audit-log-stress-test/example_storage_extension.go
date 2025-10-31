// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func ExampleAuditLogStorageExtension() {
	ctx := context.Background()

	simpleClient := sdklog.NewSimpleKeyValueStorageClient()

	storageAdapter, err := sdklog.NewAuditLogStorageExtensionAdapter(simpleClient)
	if err != nil {
		panic(err)
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, storageAdapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		SetExporterTimeout(30 * time.Second).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := &sdklog.Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(log.StringValue("User logged in successfully"))

	if err := processor.OnEmit(ctx, record); err != nil {
		fmt.Printf("Failed to emit record: %v\n", err)
	}

	time.Sleep(2 * time.Second)
	fmt.Println("Example completed")
}

func ExampleRedisStorageExtension() {
	ctx := context.Background()

	redisConfig := sdklog.RedisStorageConfig{
		Endpoint:   "localhost:6379",
		Password:   "",
		DB:         0,
		Prefix:     "audit_",
		Expiration: 24 * time.Hour,
	}

	redisClient, err := sdklog.NewRedisStorageClient(redisConfig)
	if err != nil {
		panic(err)
	}

	storageAdapter, err := sdklog.NewAuditLogStorageExtensionAdapter(redisClient)
	if err != nil {
		panic(err)
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, storageAdapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := &sdklog.Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityWarn)
	record.SetSeverityText("WARN")
	record.SetBody(log.StringValue("Suspicious activity detected"))

	if err := processor.OnEmit(ctx, record); err != nil {
		fmt.Printf("Failed to emit record: %v\n", err)
	}

	time.Sleep(2 * time.Second)
	fmt.Println("Redis storage example completed")
}

func ExampleSQLStorageExtension() {
	ctx := context.Background()

	sqlConfig := sdklog.SQLStorageConfig{
		Driver:     "sqlite3",
		Datasource: "file:audit.db?cache=shared&mode=rwc",
		TableName:  "audit_logs",
	}

	sqlClient, err := sdklog.NewSQLStorageClient(sqlConfig)
	if err != nil {
		panic(err)
	}

	storageAdapter, err := sdklog.NewAuditLogStorageExtensionAdapter(sqlClient)
	if err != nil {
		panic(err)
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, storageAdapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		SetExporterTimeout(30 * time.Second).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	record := &sdklog.Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityError)
	record.SetSeverityText("ERROR")
	record.SetBody(log.StringValue("Database connection failed"))

	if err := processor.OnEmit(ctx, record); err != nil {
		fmt.Printf("Failed to emit record: %v\n", err)
	}

	time.Sleep(2 * time.Second)
	fmt.Println("SQL storage example completed")
}

type ConsoleExporter struct{}

func (e *ConsoleExporter) Export(ctx context.Context, records []sdklog.Record) error {
	fmt.Printf("Exporting %d records:\n", len(records))
	for i, record := range records {
		fmt.Printf("  [%d] %s: %s\n", i+1, record.Severity(), record.Body())
	}
	return nil
}

func (e *ConsoleExporter) Shutdown(ctx context.Context) error {
	fmt.Println("Console exporter shutdown")
	return nil
}

func (e *ConsoleExporter) ForceFlush(ctx context.Context) error {
	fmt.Println("Console exporter force flush")
	return nil
}

func ExampleCustomStorageClient() {
	ctx := context.Background()

	customClient := &CustomStorageClient{
		storage: make(map[string][]byte),
	}

	storageAdapter, err := sdklog.NewAuditLogStorageExtensionAdapter(customClient)
	if err != nil {
		panic(err)
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, storageAdapter).
		SetScheduleDelay(500 * time.Millisecond).
		SetMaxExportBatchSize(50).
		Build()
	if err != nil {
		panic(err)
	}
	defer processor.Shutdown(ctx)

	for i := 0; i < 10; i++ {
		record := &sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetSeverityText("INFO")
		record.SetBody(log.StringValue(fmt.Sprintf("Event %d occurred", i)))

		if err := processor.OnEmit(ctx, record); err != nil {
			fmt.Printf("Failed to emit record: %v\n", err)
		}
	}

	time.Sleep(2 * time.Second)
	fmt.Printf("Custom storage client has %d items\n", customClient.Size())
	fmt.Println("Custom storage example completed")
}

type CustomStorageClient struct {
	storage map[string][]byte
}

func (c *CustomStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	value, exists := c.storage[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

func (c *CustomStorageClient) Set(ctx context.Context, key string, value []byte) error {
	c.storage[key] = value
	fmt.Printf("Custom storage: Stored key '%s' with %d bytes\n", key, len(value))
	return nil
}

func (c *CustomStorageClient) Delete(ctx context.Context, key string) error {
	delete(c.storage, key)
	fmt.Printf("Custom storage: Deleted key '%s'\n", key)
	return nil
}

func (c *CustomStorageClient) Batch(ctx context.Context, ops ...sdklog.Operation) error {
	for _, op := range ops {
		if err := op.Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *CustomStorageClient) Close(ctx context.Context) error {
	fmt.Println("Custom storage: Closing")
	c.storage = make(map[string][]byte)
	return nil
}

func (c *CustomStorageClient) Size() int {
	return len(c.storage)
}
