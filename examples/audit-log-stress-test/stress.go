// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type StorageType string

const (
	StorageTypeInMemory StorageType = "memory"
	StorageTypeFile     StorageType = "file"
	StorageTypeRedis    StorageType = "redis"
	StorageTypeSQL      StorageType = "sql"
)

type StressTestConfig struct {
	TotalLogs       int
	BatchSize       int
	ReportInterval  int
	Endpoint        string
	ScheduleDelay   time.Duration
	MaxExportBatch  int
	ExporterTimeout time.Duration
	TestRunUUID     string

	StorageType   StorageType
	StoragePath   string
	RedisEndpoint string
	RedisPassword string
	RedisDB       int
	SQLDriver     string
	SQLDatasource string
}

func DefaultStressTestConfig() *StressTestConfig {
	return &StressTestConfig{
		TotalLogs:       1_000_000,
		BatchSize:       1,
		ReportInterval:  100_000,
		Endpoint:        "http://localhost:4318",
		ScheduleDelay:   100 * time.Millisecond,
		MaxExportBatch:  1000,
		ExporterTimeout: 30 * time.Second,
		TestRunUUID:     generateUUID(),
		StorageType:     StorageTypeInMemory,
	}
}

func RunStressTest(config *StressTestConfig) error {
	if config == nil {
		config = DefaultStressTestConfig()
	}

	fmt.Println("=== Audit Log Stress Test ===")
	fmt.Println()
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Total Logs:        %d\n", config.TotalLogs)
	fmt.Printf("  Batch Size:        %d\n", config.BatchSize)
	fmt.Printf("  Report Interval:   %d\n", config.ReportInterval)
	fmt.Printf("  Test Run UUID:     %s\n", config.TestRunUUID)
	fmt.Printf("  OTLP Endpoint:     %s\n", config.Endpoint)
	fmt.Printf("  Schedule Delay:    %v\n", config.ScheduleDelay)
	fmt.Printf("  Max Export Batch:  %d\n", config.MaxExportBatch)
	fmt.Printf("  Storage Type:      %s\n", config.StorageType)
	if config.StorageType == StorageTypeFile {
		fmt.Printf("  Storage Path:      %s\n", config.StoragePath)
	} else if config.StorageType == StorageTypeRedis {
		fmt.Printf("  Redis Endpoint:    %s\n", config.RedisEndpoint)
	} else if config.StorageType == StorageTypeSQL {
		fmt.Printf("  SQL Driver:        %s\n", config.SQLDriver)
	}
	fmt.Println()

	fmt.Println("Step 1: Creating OTLP Exporter")
	otlpExporter, err := NewOTLPExporter(config.Endpoint)
	if err != nil {
		fmt.Printf("❌ Failed to create OTLP exporter: %v\n", err)
		return err
	}
	defer otlpExporter.Shutdown(context.Background())
	fmt.Println("✅ OTLP Exporter created")

	fmt.Println()
	fmt.Println("Step 2: Creating Audit Log Store")
	store, err := createStore(config)
	if err != nil {
		fmt.Printf("❌ Failed to create store: %v\n", err)
		return err
	}
	fmt.Printf("✅ %s store created\n", config.StorageType)

	fmt.Println()
	fmt.Println("Step 3: Creating Audit Processor")
	processor, err := sdklog.NewAuditLogProcessorBuilder(otlpExporter, store).
		SetScheduleDelay(config.ScheduleDelay).
		SetMaxExportBatchSize(config.MaxExportBatch).
		SetExporterTimeout(config.ExporterTimeout).
		Build()
	if err != nil {
		fmt.Printf("❌ Failed to create processor: %v\n", err)
		return err
	}
	defer processor.Shutdown(context.Background())
	fmt.Println("✅ Audit processor created")

	fmt.Println()
	fmt.Println("Step 4: Starting stress test - emitting logs")
	fmt.Println("────────────────────────────────────────────")

	startTime := time.Now()
	ctx := context.Background()

	var successCount atomic.Int64
	var errorCount atomic.Int64

	for i := 1; i <= config.TotalLogs; i++ {
		record := sdklog.Record{}
		record.SetTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetBody(log.StringValue(fmt.Sprintf("Stress test audit log #%d", i)))
		record.AddAttributes(
			log.String("test.run.uuid", config.TestRunUUID),
			log.Int64("test.log.counter", int64(i)),
			log.String("test.type", "stress_test"),
			log.Int64("test.batch", int64((i-1)/config.BatchSize)+1),
			log.Int64("test.batch.position", int64((i-1)%config.BatchSize)+1),
		)

		if err := processor.OnEmit(ctx, &record); err != nil {
			errorCount.Add(1)
		} else {
			successCount.Add(1)
		}

		if i%config.ReportInterval == 0 {
			elapsed := time.Since(startTime)
			logsPerSecond := float64(i) / elapsed.Seconds()
			fmt.Printf("📊 Progress: %d/%d logs (%.1f%%) | %.0f logs/sec | Queue: %d | Errors: %d\n",
				i, config.TotalLogs,
				float64(i)/float64(config.TotalLogs)*100,
				logsPerSecond,
				processor.GetQueueSize(),
				errorCount.Load())
		}
	}

	elapsed := time.Since(startTime)
	fmt.Println("────────────────────────────────────────────")
	fmt.Printf("✅ Emission completed in %v\n", elapsed)
	fmt.Printf("   Success: %d\n", successCount.Load())
	fmt.Printf("   Errors:  %d\n", errorCount.Load())
	fmt.Printf("   Rate:    %.0f logs/sec\n", float64(config.TotalLogs)/elapsed.Seconds())
	fmt.Printf("   Queue:   %d pending\n", processor.GetQueueSize())

	fmt.Println()
	fmt.Println("Step 5: Flushing remaining logs to OTEL Collector")
	fmt.Println("   This may take a while for large batches...")

	flushStart := time.Now()
	if err := processor.ForceFlush(context.Background()); err != nil {
		fmt.Printf("❌ Failed to flush: %v\n", err)
	} else {
		flushElapsed := time.Since(flushStart)
		fmt.Printf("✅ All logs flushed in %v\n", flushElapsed)
	}

	fmt.Println()
	fmt.Printf("   Final queue size: %d\n", processor.GetQueueSize())
	fmt.Printf("   Retry attempts:   %d\n", processor.GetRetryAttempts())

	fmt.Println()
	fmt.Println("Step 6: Test Summary")
	fmt.Println("════════════════════════════════════════════")
	fmt.Printf("🎯 Test Run UUID:     %s\n", config.TestRunUUID)
	fmt.Printf("📝 Total Logs Sent:   %d\n", successCount.Load())
	fmt.Printf("❌ Failed Logs:       %d\n", errorCount.Load())
	fmt.Printf("⏱️  Total Time:        %v\n", time.Since(startTime))
	fmt.Printf("📊 Average Rate:      %.0f logs/sec\n", float64(config.TotalLogs)/time.Since(startTime).Seconds())
	fmt.Printf("🔢 Counter Range:     1 to %d\n", config.TotalLogs)
	fmt.Println("════════════════════════════════════════════")

	fmt.Println()
	fmt.Println("🔍 Verification Instructions:")
	fmt.Println("   1. Query your observability backend for logs with:")
	fmt.Printf("      test.run.uuid = \"%s\"\n", config.TestRunUUID)
	fmt.Println("   2. Count total logs received")
	fmt.Printf("   3. Verify test.log.counter ranges from 1 to %d\n", config.TotalLogs)
	fmt.Println("   4. Check for any gaps in the counter sequence")

	return nil
}

func RunQuickStressTest() error {
	config := &StressTestConfig{
		TotalLogs:       10_000,
		BatchSize:       1,
		ReportInterval:  25_000,
		Endpoint:        "http://localhost:4318",
		ScheduleDelay:   100 * time.Millisecond,
		MaxExportBatch:  1,
		ExporterTimeout: 30 * time.Second,
		TestRunUUID:     generateUUID(),
		StorageType:     StorageTypeInMemory,
	}

	return RunStressTest(config)
}

func RunQuickStressTestWithStorage(storageType StorageType, storagePath, redisEndpoint, redisPassword string, redisDB int, sqlDriver, sqlDatasource string) error {
	config := &StressTestConfig{
		TotalLogs:       10_000,
		BatchSize:       1,
		ReportInterval:  25_000,
		Endpoint:        "http://localhost:4318",
		ScheduleDelay:   100 * time.Millisecond,
		MaxExportBatch:  1,
		ExporterTimeout: 30 * time.Second,
		TestRunUUID:     generateUUID(),
		StorageType:     storageType,
		StoragePath:     storagePath,
		RedisEndpoint:   redisEndpoint,
		RedisPassword:   redisPassword,
		RedisDB:         redisDB,
		SQLDriver:       sqlDriver,
		SQLDatasource:   sqlDatasource,
	}

	return RunStressTest(config)
}

func RunMegaStressTest() error {
	config := &StressTestConfig{
		TotalLogs:       5_000_000,
		BatchSize:       50_000,
		ReportInterval:  500_000,
		Endpoint:        "http://localhost:4318",
		ScheduleDelay:   50 * time.Millisecond,
		MaxExportBatch:  2000,
		ExporterTimeout: 60 * time.Second,
		TestRunUUID:     generateUUID(),
		StorageType:     StorageTypeInMemory,
	}

	return RunStressTest(config)
}

func RunMegaStressTestWithStorage(storageType StorageType, storagePath, redisEndpoint, redisPassword string, redisDB int, sqlDriver, sqlDatasource string) error {
	config := &StressTestConfig{
		TotalLogs:       5_000_000,
		BatchSize:       50_000,
		ReportInterval:  500_000,
		Endpoint:        "http://localhost:4318",
		ScheduleDelay:   50 * time.Millisecond,
		MaxExportBatch:  2000,
		ExporterTimeout: 60 * time.Second,
		TestRunUUID:     generateUUID(),
		StorageType:     storageType,
		StoragePath:     storagePath,
		RedisEndpoint:   redisEndpoint,
		RedisPassword:   redisPassword,
		RedisDB:         redisDB,
		SQLDriver:       sqlDriver,
		SQLDatasource:   sqlDatasource,
	}

	return RunStressTest(config)
}

func createStore(config *StressTestConfig) (sdklog.AuditLogStore, error) {
	switch config.StorageType {
	case StorageTypeInMemory:
		client := sdklog.NewSimpleKeyValueStorageClient()
		return sdklog.NewAuditLogStorageExtensionAdapter(client)

	case StorageTypeFile:
		path := config.StoragePath
		if path == "" {
			path = "./stress_test_storage.db"
		}
		client, err := sdklog.NewBoltDBStorageClient(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create file storage: %w", err)
		}
		return sdklog.NewAuditLogStorageExtensionAdapter(client)

	case StorageTypeRedis:
		redisConfig := sdklog.RedisStorageConfig{
			Endpoint:   config.RedisEndpoint,
			Password:   config.RedisPassword,
			DB:         config.RedisDB,
			Prefix:     "stress_test_",
			Expiration: 24 * time.Hour,
		}
		if redisConfig.Endpoint == "" {
			redisConfig.Endpoint = "localhost:6379"
		}
		client, err := sdklog.NewRedisStorageClient(redisConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Redis storage: %w", err)
		}
		return sdklog.NewAuditLogStorageExtensionAdapter(client)

	case StorageTypeSQL:
		sqlConfig := sdklog.SQLStorageConfig{
			Driver:     config.SQLDriver,
			Datasource: config.SQLDatasource,
			TableName:  "stress_test_logs",
		}
		if sqlConfig.Driver == "" {
			sqlConfig.Driver = "sqlite3"
		}
		if sqlConfig.Datasource == "" {
			sqlConfig.Datasource = ":memory:"
		}
		client, err := sdklog.NewSQLStorageClient(sqlConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create SQL storage: %w", err)
		}
		return sdklog.NewAuditLogStorageExtensionAdapter(client)

	default:
		return nil, fmt.Errorf("unknown storage type: %s", config.StorageType)
	}
}

func generateUUID() string {
	return fmt.Sprintf("%d-%04d-%04d-%04d-%012d",
		time.Now().Unix(),
		time.Now().Nanosecond()%10000,
		time.Now().Nanosecond()/10000%10000,
		time.Now().Nanosecond()/100000%10000,
		time.Now().UnixNano()%1000000000000)
}
