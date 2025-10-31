// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel/sdk/log"
)

func TestStressTest(t *testing.T) {
	if os.Getenv("RUN_STRESS_TEST") == "" {
		t.Skip("Skipping stress test - set RUN_STRESS_TEST=1 to run")
	}

	config := &log.StressTestConfig{
		TotalLogs:      1_000_000,
		BatchSize:      10_000,
		ReportInterval: 100_000,
		Endpoint:       "http://localhost:4318",
	}

	if err := log.RunStressTest(config); err != nil {
		t.Fatalf("Stress test failed: %v", err)
	}
}

func TestQuickStressTest(t *testing.T) {
	if os.Getenv("RUN_STRESS_TEST") == "" {
		t.Skip("Skipping quick stress test - set RUN_STRESS_TEST=1 to run")
	}

	if err := log.RunQuickStressTest(); err != nil {
		t.Fatalf("Quick stress test failed: %v", err)
	}
}
