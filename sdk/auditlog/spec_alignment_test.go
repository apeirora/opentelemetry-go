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
	mu          sync.Mutex
	batches     [][]Record
	exportError error
}

func (e *captureExporter) Export(ctx context.Context, records []Record) (ExportResult, error) {
	_ = ctx
	e.mu.Lock()
	exportErr := e.exportError
	e.mu.Unlock()
	if exportErr != nil {
		return ExportResult{}, exportErr
	}
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

func TestZeroConfigProviderAcceptsRecordWithoutIntegrity(t *testing.T) {
	provider := NewAuditLoggerProvider()
	rec := minimalAuditRecordNoTarget()
	if err := validateRequiredAuditRecord(rec, provider); err != nil {
		t.Fatalf("zero-config provider should not require integrity: %v", err)
	}
	result := provider.Logger("zero").EmitWithResult(context.Background(), rec)
	if result.StatusCode >= 400 {
		t.Fatalf("expected emit to succeed without integrity, got status %d reason %q", result.StatusCode, result.Reason)
	}
}

func TestExportUsesSpecIntegrityAttributesOnly(t *testing.T) {
	key := []byte("spec-export-key")
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

	provider := NewAuditLoggerProvider(
		WithAuditRecordProcessor(processor),
		WithAuditHMACVerificationKey(key),
		WithAuditAutoSignIntegrity(AuditIntegrityHMAC),
		WithAuditExportIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = testAuditRecordID(10)
	result := provider.Logger("spec").EmitWithResult(context.Background(), rec)
	if result.StatusCode >= 400 {
		t.Fatalf("emit failed: status %d reason %q", result.StatusCode, result.Reason)
	}
	batch := capture.lastBatch()
	if len(batch) != 1 {
		t.Fatalf("expected one exported batch, got %d", len(batch))
	}
	attrs := attrKeysFromRecord(batch[0])
	for _, legacy := range []string{"audit.hmac", "audit.signature", "audit.hash", "audit.key_id", "audit.prev_hash"} {
		if attrs[legacy] {
			t.Fatalf("legacy attribute %q must not be exported", legacy)
		}
	}
	if !attrs["audit.integrity.value"] {
		t.Fatal("expected audit.integrity.value on exported record")
	}
	if attrs["audit.integrity.algorithm"] {
		t.Fatal("audit.integrity.algorithm must be on resource, not record attributes")
	}
	res := batch[0].Resource()
	if res == nil {
		t.Fatal("expected resource on exported record")
	}
	resAttrs := res.Attributes()
	var hasAlg bool
	for _, kv := range resAttrs {
		if string(kv.Key) == auditAttrIntegrityAlgorithm {
			hasAlg = true
			if kv.Value.AsString() != "HMAC-SHA256" {
				t.Fatalf("resource algorithm: got %q", kv.Value.AsString())
			}
		}
		if string(kv.Key) == auditAttrIntegrityCertificate {
			t.Fatal("audit.integrity.certificate must not be set for HMAC algorithms")
		}
	}
	if !hasAlg {
		t.Fatal("expected audit.integrity.algorithm on resource")
	}
}

func TestAuditRecordSpecNormalization(t *testing.T) {
	rec := minimalAuditRecordNoTarget()
	rec.Action = " read "
	rec.ActorType = "USER"
	rec.Outcome = "SUCCESS"
	normalizeAuditRecordFields(&rec)
	if rec.Action != "READ" {
		t.Fatalf("action: got %q", rec.Action)
	}
	if rec.ActorType != "user" {
		t.Fatalf("actor type: got %q", rec.ActorType)
	}
	if rec.Outcome != "success" {
		t.Fatalf("outcome: got %q", rec.Outcome)
	}
}

func TestEmitClearsSeverityOnExportedRecord(t *testing.T) {
	provider := NewAuditLoggerProvider()
	rec := minimalAuditRecordNoTarget()
	rec.SetSeverity(log.SeverityError)
	rec.SetSeverityText("ERROR")
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
	provider = NewAuditLoggerProvider(WithAuditRecordProcessor(processor))
	result := provider.Logger("sev").EmitWithResult(context.Background(), rec)
	if result.StatusCode >= 400 {
		t.Fatalf("emit failed: %d %q", result.StatusCode, result.Reason)
	}
	batch := capture.lastBatch()
	if len(batch) != 1 {
		t.Fatal("expected exported batch")
	}
	if batch[0].Severity() != 0 {
		t.Fatalf("severity should be cleared, got %v", batch[0].Severity())
	}
	if batch[0].SeverityText() != "" {
		t.Fatalf("severity text should be cleared, got %q", batch[0].SeverityText())
	}
}

func TestSyncEmitFailsWhenRetryBudgetExhausted(t *testing.T) {
	key := []byte("drop-sync-key")
	failExporter := &captureExporter{}
	store := NewAuditLogInMemoryStore()
	builder, err := NewAuditLogProcessorBuilder(failExporter, store)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := builder.
		SetMaxExportBatchSize(1).
		SetRetryPolicy(RetryPolicy{
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			MaxAttempts:       1,
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = processor.Shutdown(context.Background()) })

	provider := NewAuditLoggerProvider(
		WithAuditRecordProcessor(processor),
		WithAuditHMACVerificationKey(key),
		WithAuditAutoSignIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = testAuditRecordID(11)

	failExporter.mu.Lock()
	failExporter.exportError = context.DeadlineExceeded
	failExporter.mu.Unlock()

	result := provider.Logger("drop").EmitWithResult(context.Background(), rec)
	if result.StatusCode < 400 {
		t.Fatalf("expected failure when export retries exhausted, got status %d", result.StatusCode)
	}
}

func attrKeysFromRecord(rec Record) map[string]bool {
	out := make(map[string]bool)
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		out[string(kv.Key)] = true
		return true
	})
	return out
}

func TestValidateAuditRecordTargetOptional(t *testing.T) {
	key := []byte("target-optional-key")
	p := NewAuditLoggerProvider(
		WithAuditHMACVerificationKey(key),
		WithAuditRequiredIntegrity(AuditIntegrityHMAC),
	)
	rec := minimalAuditRecordNoTarget()
	signed, err := SignAuditRecordHMAC(rec, key, "sha256")
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
	signed, err := SignAuditRecordHMAC(rec, key, "sha256")
	if err != nil {
		t.Fatal(err)
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
	if len(mac) != 32 {
		t.Fatalf("expected 32-byte HMAC, got %d", len(mac))
	}
}

func TestIntegrityHashUsesJCS_SHA256(t *testing.T) {
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = testAuditRecordID(12)
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
	syncID := testAuditRecordID(13)
	rec.RecordID = syncID

	receipt, err := provider.Logger("sync").Emit(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecordID != syncID {
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
		RecordID:   testAuditRecordID(14),
		Body:       log.StringValue(`{"ok":true}`),
	}
	receipt, err := got.Logger("api").Emit(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecordID != testAuditRecordID(14) {
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
	if enriched.IntegrityValue == "" {
		t.Fatal("expected audit.integrity.value after enrich")
	}
	if enriched.IntegrityAlgorithm != "HMAC-SHA256" {
		t.Fatalf("expected HMAC-SHA256, got %q", enriched.IntegrityAlgorithm)
	}
}

func TestExportOKReturnsReceiptPerRecord(t *testing.T) {
	rec := minimalAuditRecordNoTarget()
	rec.RecordID = testAuditRecordID(15)
	rec2 := minimalAuditRecordNoTarget()
	rec2.RecordID = testAuditRecordID(16)
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
	rec.RecordID = testAuditRecordID(17)
	rec.AddAttributes(
		log.String(auditAttrIntegrityValue, base64.StdEncoding.EncodeToString([]byte("fake"))),
		log.String(auditAttrIntegrityAlgorithm, "HMAC-SHA256"),
	)
	signed, err := SignAuditRecordHMAC(rec, key, "sha256")
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

