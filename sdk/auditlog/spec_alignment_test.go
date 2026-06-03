// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/audit"
	auditglobal "go.opentelemetry.io/otel/audit/global"
	"go.opentelemetry.io/otel/log"
)

type captureExporter struct {
	mu      sync.Mutex
	batches [][]Record
}

func (e *captureExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	_ = ctx
	e.mu.Lock()
	copied := make([]Record, len(records))
	for i := range records {
		copied[i] = records[i].Clone()
	}
	e.batches = append(e.batches, copied)
	e.mu.Unlock()
	return ExportOK(records), nil
}

func (e *captureExporter) lastBatch() []Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.batches) == 0 {
		return nil
	}
	return e.batches[len(e.batches)-1]
}

func (e *captureExporter) Shutdown(context.Context) error  { return nil }
func (e *captureExporter) ForceFlush(context.Context) error { return nil }

func TestBuilderDefaultWaitOnExportTrue(t *testing.T) {
	exporter := &captureExporter{}
	store := NewAuditLogInMemoryStore()
	builder, err := NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		t.Fatal(err)
	}
	if !builder.GetConfig().WaitOnExport {
		t.Fatal("expected builder default WaitOnExport true for synchronous delivery")
	}
}

func TestValidateAuditRecordTargetOptional(t *testing.T) {
	key := []byte("target-optional-key")
	p := NewAuditLoggerProvider(
		WithAuditHMACVerificationKey(key),
		WithAuditRequiredIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	signed, err := SignAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRequiredAuditRecord(signed, p); err != nil {
		t.Fatalf("target should be optional: %v", err)
	}
}

func TestSignAuditRecordHMACSetsSpecIntegrityFields(t *testing.T) {
	key := []byte("integrity-spec-key")
	rec := minimalAuditRecordNoTarget()
	signed, err := SignAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	if signed.HMAC == "" {
		t.Fatal("expected legacy audit.hmac hex")
	}
	if signed.IntegrityValue == "" {
		t.Fatal("expected audit.integrity.value")
	}
	if signed.IntegrityAlgorithm != "HMAC-SHA256" {
		t.Fatalf("expected HMAC-SHA256, got %q", signed.IntegrityAlgorithm)
	}
	mac, err := decodeIntegrityValueHMAC(signed.IntegrityValue)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(mac) != signed.HMAC {
		t.Fatalf("integrity.value must match hmac bytes: value=%q hmac=%q", signed.IntegrityValue, signed.HMAC)
	}
}

func TestIntegrityHashUsesJCS_SHA256(t *testing.T) {
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = "jcs-record-1"
	payload, err := jcsSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	got, err := integrityHashForAuditRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("integrity hash: got %s want %s", got, want)
	}
}

func TestSyncEmitDeliveredReturnsAuditReceipt(t *testing.T) {
	key := []byte("sync-receipt-key")
	capture := &captureExporter{}
	store := NewAuditLogInMemoryStore()
	builder, err := NewAuditLogProcessorBuilder(capture, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := builder.
		SetScheduleDelay(5 * time.Millisecond).
		SetMaxExportBatchSize(1).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = processor.Shutdown(context.Background()) })

	provider := NewAuditLoggerProvider(
		WithAuditRecordProcessor(processor),
		WithAuditHMACVerificationKey(key),
		WithAuditRequiredIntegrity(AuditIntegrityHMAC),
		WithAuditAutoSignIntegrity(AuditIntegrityHMAC),
		WithAuditExportIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = "sync-receipt-id"

	receipt, err := provider.Logger("sync").Emit(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecordID != "sync-receipt-id" {
		t.Fatalf("receipt record id: got %q", receipt.RecordID)
	}
	if receipt.IntegrityHash == "" {
		t.Fatal("expected non-empty IntegrityHash on delivered receipt")
	}
	if receipt.SinkTimestamp.IsZero() {
		t.Fatal("expected SinkTimestamp on delivered receipt")
	}
	wantHash, err := integrityHashForAuditRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.IntegrityHash != wantHash {
		t.Fatalf("receipt hash mismatch: got %s want %s", receipt.IntegrityHash, wantHash)
	}
	if len(capture.lastBatch()) != 1 {
		t.Fatalf("expected one exported batch, got %d", len(capture.batches))
	}
}

func TestSdkAuditProviderEmitAndGlobalAccessor(t *testing.T) {
	key := []byte("global-api-key")
	capture := &captureExporter{}
	store := NewAuditLogInMemoryStore()
	builder, err := NewAuditLogProcessorBuilder(capture, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := builder.SetMaxExportBatchSize(1).Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = processor.Shutdown(context.Background()) })

	sdkProv := NewSdkAuditProvider(
		WithAuditHMACVerificationKey(key),
		WithAuditRequiredIntegrity(AuditIntegrityHMAC),
		WithAuditAutoSignIntegrity(AuditIntegrityHMAC),
		WithAuditRecordProcessor(processor),
	)
	auditglobal.SetAuditProvider(sdkProv)
	t.Cleanup(func() { auditglobal.SetAuditProvider(nil) })

	got := auditglobal.GetAuditProvider()
	if got == nil {
		t.Fatal("expected global audit provider")
	}

	rec := audit.AuditRecord{
		Timestamp:  time.Now().UTC(),
		EventName:  "api.emit",
		Actor:      log.StringValue("actor-1"),
		ActorType:  "user",
		Action:     "READ",
		Outcome:    "success",
		RecordID:   "api-record-1",
		Body:       log.StringValue(`{"ok":true}`),
	}
	receipt, err := got.Logger("api").Emit(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecordID != "api-record-1" {
		t.Fatalf("api receipt record id: got %q", receipt.RecordID)
	}
	if receipt.IntegrityHash == "" {
		t.Fatal("expected IntegrityHash from api Emit")
	}
}

func TestProviderEnrichIntegritySetsSpecFields(t *testing.T) {
	key := []byte("enrich-spec-key")
	provider := NewAuditLoggerProvider(
		WithAuditHMACVerificationKey(key),
		WithAuditRequiredIntegrity(AuditIntegrityHMAC),
		WithAuditAutoSignIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	enriched, err := provider.enrichIntegrity(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.HMAC == "" {
		t.Fatal("expected audit.hmac after enrich")
	}
	if enriched.IntegrityValue == "" {
		t.Fatal("expected audit.integrity.value after enrich")
	}
	if enriched.IntegrityAlgorithm != "HMAC-SHA256" {
		t.Fatalf("expected HMAC-SHA256, got %q", enriched.IntegrityAlgorithm)
	}
}

func TestExportOKReturnsReceiptPerRecord(t *testing.T) {
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = "receipt-a"
	rec2 := minimalAuditRecordNoTarget()
	rec2.RecordID = "receipt-b"
	result := ExportOK([]Record{rec.Record, rec2.Record})
	if len(result.Receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(result.Receipts))
	}
	for i, r := range result.Receipts {
		if r.IntegrityHash == "" {
			t.Fatalf("receipt[%d]: empty integrity hash", i)
		}
		if r.SinkTimestamp.IsZero() {
			t.Fatalf("receipt[%d]: empty sink timestamp", i)
		}
	}
}

func TestIntegrityValueExcludesIntegrityAttrsFromJCS(t *testing.T) {
	key := []byte("jcs-exclude-key")
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = "jcs-exclude-1"
	rec.AddAttributes(
		log.String(auditAttrIntegrityValue, base64.StdEncoding.EncodeToString([]byte("fake"))),
		log.String(auditAttrIntegrityAlgorithm, "HMAC-SHA256"),
	)
	signed, err := SignAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := jcsSigningPayload(signed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "audit.integrity.value") {
		t.Fatal("JCS payload must not include audit.integrity.* attributes")
	}
}

func minimalAuditRecordNoTarget() AuditRecord {
	now := time.Now().UTC()
	base := Record{}
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetBody(log.StringValue(`{"event":"test"}`))
	return AuditRecord{
		Record:    base,
		EventName: "test.event",
		Actor:     log.StringValue("actor"),
		ActorType: "user",
		Action:    "READ",
		Outcome:   "success",
	}
}

