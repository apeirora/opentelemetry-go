// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	totalLogs := flag.Int("logs", 1_000_000, "Total number of logs to send")
	batchSize := flag.Int("batch", 10_000, "Batch size for grouping")
	reportInterval := flag.Int("report", 100_000, "Progress report interval")
	endpoint := flag.String("endpoint", "http://localhost:4318", "OTLP endpoint URL")
	scheduleDelay := flag.Duration("delay", 100*time.Millisecond, "Schedule delay between exports")
	maxExportBatch := flag.Int("export-batch", 1000, "Maximum export batch size")
	testUUID := flag.String("uuid", "", "Test run UUID (auto-generated if not provided)")
	quick := flag.Bool("quick", false, "Run quick test (100k logs)")
	mega := flag.Bool("mega", false, "Run mega test (5M logs)")

	storageType := flag.String("storage", "memory", "Storage type: memory, file, redis, sql")
	storagePath := flag.String("storage-path", "./stress_test_storage.db", "Storage path for file storage")
	redisEndpoint := flag.String("redis-endpoint", "localhost:6379", "Redis endpoint")
	redisPassword := flag.String("redis-password", "", "Redis password")
	redisDB := flag.Int("redis-db", 0, "Redis database number")
	sqlDriver := flag.String("sql-driver", "sqlite3", "SQL driver: sqlite3, postgres, mysql")
	sqlDatasource := flag.String("sql-datasource", ":memory:", "SQL datasource connection string")
	//go run main.go -quick -storage redis -redis-endpoint localhost:6379
	flag.Parse()

	if *quick {
		fmt.Println("Running quick stress test (10k logs)...")
		if err := RunQuickStressTestWithStorage(
			StorageType(*storageType),
			*storagePath,
			*redisEndpoint,
			*redisPassword,
			*redisDB,
			*sqlDriver,
			*sqlDatasource,
		); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *mega {
		fmt.Println("Running mega stress test (5M logs)...")
		if err := RunMegaStressTestWithStorage(
			StorageType(*storageType),
			*storagePath,
			*redisEndpoint,
			*redisPassword,
			*redisDB,
			*sqlDriver,
			*sqlDatasource,
		); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	config := &StressTestConfig{
		TotalLogs:       *totalLogs,
		BatchSize:       *batchSize,
		ReportInterval:  *reportInterval,
		Endpoint:        *endpoint,
		ScheduleDelay:   *scheduleDelay,
		MaxExportBatch:  *maxExportBatch,
		ExporterTimeout: 30 * time.Second,
		StorageType:     StorageType(*storageType),
		StoragePath:     *storagePath,
		RedisEndpoint:   *redisEndpoint,
		RedisPassword:   *redisPassword,
		RedisDB:         *redisDB,
		SQLDriver:       *sqlDriver,
		SQLDatasource:   *sqlDatasource,
	}

	if *testUUID != "" {
		config.TestRunUUID = *testUUID
	}

	if err := RunStressTest(config); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
