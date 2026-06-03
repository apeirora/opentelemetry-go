// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	"go.opentelemetry.io/otel/sdk/auditlog/otlpexport"
	"go.opentelemetry.io/otel/sdk/auditlog/stresstest/mockreceiver"
	"go.opentelemetry.io/otel/sdk/log/logtest"
)

func TestNewHTTPPostsToV1AuditPath(t *testing.T) {
	recv, err := mockreceiver.Start(mockreceiver.Config{
		URLPath:        "/v1/audit",
		StartAccepting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = recv.Close(ctx)
	}()

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(recv.HostPort()),
		otlpexport.WithInsecure(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	store := auditlog.NewAuditLogInMemoryStore()
	pb, err := auditlog.NewAuditLogProcessorBuilder(exp, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := pb.
		SetScheduleDelay(5 * time.Millisecond).
		SetMaxExportBatchSize(1).
		SetWaitOnExport(true).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = processor.Shutdown(context.Background()) }()

	key := []byte("otlp-v1-audit-key")
	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(key),
		auditlog.WithAuditRequiredIntegrity(auditlog.AuditIntegrityHMAC),
		auditlog.WithAuditAutoSignIntegrity(auditlog.AuditIntegrityHMAC),
	)

	now := time.Now().UTC()
	base := logtest.RecordFactory{
		Timestamp:         now,
		ObservedTimestamp: now,
		Body:              log.StringValue(`{"n":1}`),
		Attributes:        []log.KeyValue{log.String("audit.record.id", "otlp-path-test-1")},
	}.NewRecord()
	rec := auditlog.AuditRecord{
		Record:    base,
		EventName: "otlp.path.test",
		Actor:     log.StringValue("actor"),
		ActorType: "user",
		Action:    "EMIT",
		Outcome:   "success",
		RecordID:  "otlp-path-test-1",
	}

	if _, err := provider.Logger("otlp").Emit(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if recv.UniqueRecordCount() != 1 {
		t.Fatalf("expected 1 record at /v1/audit receiver, got %d (requests=%d failed=%d)",
			recv.UniqueRecordCount(), recv.RequestsTotal(), recv.FailedRequests())
	}
}

func TestNewHTTPWrongPathNotAccepted(t *testing.T) {
	recv, err := mockreceiver.Start(mockreceiver.Config{
		URLPath:        "/v1/audit",
		StartAccepting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = recv.Close(ctx)
	}()

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(recv.HostPort()),
		otlpexport.WithInsecure(),
		otlpexport.WithURLPath("/v1/logs"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	store := auditlog.NewAuditLogInMemoryStore()
	pb, err := auditlog.NewAuditLogProcessorBuilder(exp, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := pb.
		SetScheduleDelay(5 * time.Millisecond).
		SetMaxExportBatchSize(1).
		SetWaitOnExport(true).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = processor.Shutdown(context.Background()) }()

	key := []byte("otlp-wrong-path-key")
	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(key),
		auditlog.WithAuditRequiredIntegrity(auditlog.AuditIntegrityHMAC),
		auditlog.WithAuditAutoSignIntegrity(auditlog.AuditIntegrityHMAC),
	)

	now := time.Now().UTC()
	base := logtest.RecordFactory{
		Timestamp:         now,
		ObservedTimestamp: now,
		Body:              log.StringValue(`{"n":2}`),
		Attributes:        []log.KeyValue{log.String("audit.record.id", "otlp-wrong-path-1")},
	}.NewRecord()
	rec := auditlog.AuditRecord{
		Record:    base,
		EventName: "otlp.path.fail",
		Actor:     log.StringValue("actor"),
		ActorType: "user",
		Action:    "EMIT",
		Outcome:   "success",
		RecordID:  "otlp-wrong-path-1",
	}

	_, err = provider.Logger("otlp").Emit(context.Background(), rec)
	if err == nil {
		t.Fatal("expected export error when posting to /v1/logs against /v1/audit receiver")
	}
	if recv.UniqueRecordCount() != 0 {
		t.Fatalf("receiver must not accept wrong path traffic, got %d records", recv.UniqueRecordCount())
	}
}
