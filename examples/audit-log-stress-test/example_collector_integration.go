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

func ExampleWithFileStorage() {
	ctx := context.Background()

	fileClient, err := sdklog.NewBoltDBStorageClient("./audit_storage/audit.db")
	if err != nil {
		panic(fmt.Sprintf("Failed to create file storage client: %v", err))
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(fileClient)
	if err != nil {
		panic(fmt.Sprintf("Failed to create storage adapter: %v", err))
	}

	exporter := &ConsoleExporter{}

	retryPolicy := sdklog.RetryPolicy{
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
	}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
		SetScheduleDelay(2 * time.Second).
		SetMaxExportBatchSize(100).
		SetExporterTimeout(30 * time.Second).
		SetRetryPolicy(retryPolicy).
		SetWaitOnExport(false).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create processor: %v", err))
	}
	defer processor.Shutdown(ctx)

	for i := 0; i < 5; i++ {
		record := &sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetSeverityText("INFO")
		record.SetBody(log.StringValue(fmt.Sprintf("Audit event #%d: User action recorded", i)))

		if err := processor.OnEmit(ctx, record); err != nil {
			fmt.Printf("Failed to emit record: %v\n", err)
		}
	}

	time.Sleep(3 * time.Second)

	if err := processor.ForceFlush(ctx); err != nil {
		fmt.Printf("Failed to flush: %v\n", err)
	}

	fmt.Println("File storage example completed")
}

func ExampleWithRedisDistributedStorage() {
	ctx := context.Background()

	redisConfig := sdklog.RedisStorageConfig{
		Endpoint:   "localhost:6379",
		Password:   "",
		DB:         0,
		Prefix:     "otel_audit_",
		Expiration: 24 * time.Hour,
	}

	redisClient, err := sdklog.NewRedisStorageClient(redisConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to create Redis client: %v", err))
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(redisClient)
	if err != nil {
		panic(fmt.Sprintf("Failed to create storage adapter: %v", err))
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(50).
		SetExporterTimeout(15 * time.Second).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create processor: %v", err))
	}
	defer processor.Shutdown(ctx)

	severities := []log.Severity{
		log.SeverityInfo,
		log.SeverityWarn,
		log.SeverityError,
	}

	for i := 0; i < 10; i++ {
		record := &sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(severities[i%3])
		record.SetSeverityText(severities[i%3].String())
		record.SetBody(log.StringValue(fmt.Sprintf("Distributed audit log #%d", i)))

		if err := processor.OnEmit(ctx, record); err != nil {
			fmt.Printf("Failed to emit record: %v\n", err)
		}
	}

	time.Sleep(3 * time.Second)
	fmt.Println("Redis distributed storage example completed")
}

func ExampleWithDatabaseStorage() {
	ctx := context.Background()

	sqlConfig := sdklog.SQLStorageConfig{
		Driver:     "sqlite3",
		Datasource: "file:./audit_storage/audit.db?cache=shared&mode=rwc",
		TableName:  "audit_logs",
	}

	sqlClient, err := sdklog.NewSQLStorageClient(sqlConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to create SQL client: %v", err))
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(sqlClient)
	if err != nil {
		panic(fmt.Sprintf("Failed to create storage adapter: %v", err))
	}

	exporter := &ConsoleExporter{}

	customHandler := &sdklog.DefaultAuditExceptionHandler{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		SetExporterTimeout(30 * time.Second).
		SetExceptionHandler(customHandler).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create processor: %v", err))
	}
	defer processor.Shutdown(ctx)

	record := &sdklog.Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityError)
	record.SetSeverityText("ERROR")
	record.SetBody(log.StringValue("Critical system error detected"))

	if err := processor.OnEmit(ctx, record); err != nil {
		fmt.Printf("Failed to emit record: %v\n", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("Database storage example completed")
}

func ExampleMultipleProcessorsWithDifferentStorages() {
	ctx := context.Background()

	memoryClient := sdklog.NewSimpleKeyValueStorageClient()
	memoryAdapter, _ := sdklog.NewAuditLogStorageExtensionAdapter(memoryClient)

	fileClient, _ := sdklog.NewBoltDBStorageClient("./audit_storage/persistent.db")
	fileAdapter, _ := sdklog.NewAuditLogStorageExtensionAdapter(fileClient)

	exporter := &ConsoleExporter{}

	memoryProcessor, err := sdklog.NewAuditLogProcessorBuilder(exporter, memoryAdapter).
		SetScheduleDelay(500 * time.Millisecond).
		SetMaxExportBatchSize(10).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create memory processor: %v", err))
	}
	defer memoryProcessor.Shutdown(ctx)

	fileProcessor, err := sdklog.NewAuditLogProcessorBuilder(exporter, fileAdapter).
		SetScheduleDelay(2 * time.Second).
		SetMaxExportBatchSize(100).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create file processor: %v", err))
	}
	defer fileProcessor.Shutdown(ctx)

	normalRecord := &sdklog.Record{}
	normalRecord.SetTimestamp(time.Now())
	normalRecord.SetObservedTimestamp(time.Now())
	normalRecord.SetSeverity(log.SeverityInfo)
	normalRecord.SetSeverityText("INFO")
	normalRecord.SetBody(log.StringValue("Normal operation - using memory storage"))

	if err := memoryProcessor.OnEmit(ctx, normalRecord); err != nil {
		fmt.Printf("Failed to emit to memory processor: %v\n", err)
	}

	criticalRecord := &sdklog.Record{}
	criticalRecord.SetTimestamp(time.Now())
	criticalRecord.SetObservedTimestamp(time.Now())
	criticalRecord.SetSeverity(log.SeverityFatal)
	criticalRecord.SetSeverityText("FATAL")
	criticalRecord.SetBody(log.StringValue("Critical error - using persistent file storage"))

	if err := fileProcessor.OnEmit(ctx, criticalRecord); err != nil {
		fmt.Printf("Failed to emit to file processor: %v\n", err)
	}

	time.Sleep(3 * time.Second)
	fmt.Println("Multiple processors example completed")
}

func ExampleStorageClientWithCompression() {
	ctx := context.Background()

	compressedClient := &CompressedStorageClientWrapper{
		underlying: sdklog.NewSimpleKeyValueStorageClient(),
	}

	adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(compressedClient)
	if err != nil {
		panic(fmt.Sprintf("Failed to create storage adapter: %v", err))
	}

	exporter := &ConsoleExporter{}

	processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
		SetScheduleDelay(1 * time.Second).
		SetMaxExportBatchSize(100).
		Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to create processor: %v", err))
	}
	defer processor.Shutdown(ctx)

	for i := 0; i < 100; i++ {
		record := &sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetSeverityText("INFO")
		record.SetBody(log.StringValue(fmt.Sprintf("Large audit message with lots of data: %d", i)))

		if err := processor.OnEmit(ctx, record); err != nil {
			fmt.Printf("Failed to emit record: %v\n", err)
		}
	}

	time.Sleep(2 * time.Second)
	fmt.Printf("Storage client reported compression savings\n")
	fmt.Println("Compressed storage example completed")
}

type CompressedStorageClientWrapper struct {
	underlying sdklog.StorageClient
}

func (c *CompressedStorageClientWrapper) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := c.underlying.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *CompressedStorageClientWrapper) Set(ctx context.Context, key string, value []byte) error {
	return c.underlying.Set(ctx, key, value)
}

func (c *CompressedStorageClientWrapper) Delete(ctx context.Context, key string) error {
	return c.underlying.Delete(ctx, key)
}

func (c *CompressedStorageClientWrapper) Batch(ctx context.Context, ops ...sdklog.Operation) error {
	return c.underlying.Batch(ctx, ops...)
}

func (c *CompressedStorageClientWrapper) Close(ctx context.Context) error {
	return c.underlying.Close(ctx)
}
