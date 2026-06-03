// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package global_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/audit"
	auditglobal "go.opentelemetry.io/otel/audit/global"
	"go.opentelemetry.io/otel/log"
)

type stubAuditProvider struct{}

func (stubAuditProvider) Logger(string, ...audit.LoggerOption) audit.AuditLogger {
	return stubAuditLogger{}
}

func (stubAuditProvider) Shutdown(context.Context) error { return nil }

func (stubAuditProvider) ForceFlush(context.Context) error { return nil }

type stubAuditLogger struct{}

func (stubAuditLogger) Emit(context.Context, audit.AuditRecord) (audit.AuditReceipt, error) {
	return audit.AuditReceipt{
		RecordID:      "stub-id",
		IntegrityHash: "stub-hash",
		SinkTimestamp: time.Now().UTC(),
	}, nil
}

func TestSetGetAuditProvider(t *testing.T) {
	auditglobal.SetAuditProvider(nil)
	if auditglobal.GetAuditProvider() != nil {
		t.Fatal("expected nil provider after reset")
	}
	stub := stubAuditProvider{}
	auditglobal.SetAuditProvider(stub)
	t.Cleanup(func() { auditglobal.SetAuditProvider(nil) })
	got := auditglobal.GetAuditProvider()
	if got == nil {
		t.Fatal("expected non-nil provider")
	}
	receipt, err := got.Logger("t").Emit(context.Background(), audit.AuditRecord{
		Timestamp: time.Now().UTC(),
		EventName: "test",
		Actor:     log.StringValue("a"),
		ActorType: "user",
		Action:    "READ",
		Outcome:   "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecordID != "stub-id" {
		t.Fatalf("record id: got %q", receipt.RecordID)
	}
}
