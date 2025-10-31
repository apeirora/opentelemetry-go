// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel/sdk/log"
)

func main() {
	totalLogs := flag.Int("logs", 1_000_000, "Total number of logs to send")
	batchSize := flag.Int("batch", 10_000, "Batch size for grouping")
	reportInterval := flag.Int("report", 100_000, "Progress report interval")
	endpoint := flag.String("endpoint", "http://localhost:4318", "OTLP endpoint URL")
	scheduleDelay := flag.Duration("delay", 100*time.Millisecond, "Schedule delay between exports")
	maxExportBatch := flag.Int("export-batch", 1000, "Maximum export batch size")
	quick := flag.Bool("quick", false, "Run quick test (100k logs)")
	mega := flag.Bool("mega", false, "Run mega test (5M logs)")

	flag.Parse()

	if *quick {
		fmt.Println("Running quick stress test (100k logs)...")
		if err := log.RunQuickStressTest(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *mega {
		fmt.Println("Running mega stress test (5M logs)...")
		if err := log.RunMegaStressTest(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	config := &log.StressTestConfig{
		TotalLogs:       *totalLogs,
		BatchSize:       *batchSize,
		ReportInterval:  *reportInterval,
		Endpoint:        *endpoint,
		ScheduleDelay:   *scheduleDelay,
		MaxExportBatch:  *maxExportBatch,
		ExporterTimeout: 30 * time.Second,
	}

	if err := log.RunStressTest(config); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
