// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
)

type testGatedExporter struct {
	allowSuccess atomic.Bool
	mu           sync.Mutex
	exported     []auditlog.Record
}

func (e *testGatedExporter) Export(ctx context.Context, records []auditlog.Record) error {
	if !e.allowSuccess.Load() {
		return fmt.Errorf("export blocked")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := make([]auditlog.Record, len(records))
	copy(copied, records)
	e.exported = append(e.exported, copied...)
	return nil
}

func (e *testGatedExporter) Shutdown(ctx context.Context) error  { return nil }
func (e *testGatedExporter) ForceFlush(ctx context.Context) error { return nil }

func (e *testGatedExporter) ExportedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.exported)
}

type testMockExporter struct {
	mu       sync.Mutex
	exported [][]auditlog.Record
}

func (m *testMockExporter) Export(ctx context.Context, records []auditlog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]auditlog.Record, len(records))
	copy(copied, records)
	m.exported = append(m.exported, copied)
	return nil
}

func (m *testMockExporter) Shutdown(ctx context.Context) error  { return nil }
func (m *testMockExporter) ForceFlush(ctx context.Context) error { return nil }

func (m *testMockExporter) ExportCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.exported)
}

func TestAuditIntegrationProviderAsyncFileRecovery(t *testing.T) {
	exporter := &testGatedExporter{}
	hmacKey := []byte("integration-key")
	storeDir := t.TempDir()

	firstStore, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("failed to create first file store: %v", err)
	}

	firstProcessor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      firstStore,
		ExceptionHandler:   &auditlog.DefaultAuditExceptionHandler{},
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    50 * time.Millisecond,
		RetryPolicy: auditlog.RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		DeliveryMode: auditlog.AuditDeliveryModeAsyncStoreRetry,
	})
	if err != nil {
		t.Fatalf("failed to create first processor: %v", err)
	}

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(firstProcessor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("audit-integration")

	const total = 3
	for i := 0; i < total; i++ {
		record := makeValidAuditRecordForTest(t, fmt.Sprintf("recovery-%d", i), hmacKey)
		result := logger.EmitWithResult(context.Background(), record)
		if result.StatusCode != 202 {
			t.Fatalf("expected queued result 202, got %d (%s)", result.StatusCode, result.Reason)
		}
	}

	time.Sleep(40 * time.Millisecond)
	if err := firstProcessor.Shutdown(context.Background()); err == nil {
		t.Fatalf("expected shutdown error while exporter is blocked")
	}

	persistedAfterFailure, err := firstStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed reading persisted records: %v", err)
	}
	if len(persistedAfterFailure) != total {
		t.Fatalf("expected %d persisted records, got %d", total, len(persistedAfterFailure))
	}

	exporter.allowSuccess.Store(true)

	secondStore, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("failed to reopen file store: %v", err)
	}
	secondProcessor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      secondStore,
		ExceptionHandler:   &auditlog.DefaultAuditExceptionHandler{},
		ScheduleDelay:      5 * time.Millisecond,
		MaxExportBatchSize: 1,
		ExporterTimeout:    time.Second,
		RetryPolicy: auditlog.RetryPolicy{
			InitialBackoff:    1 * time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		DeliveryMode: auditlog.AuditDeliveryModeAsyncStoreRetry,
	})
	if err != nil {
		t.Fatalf("failed to create second processor: %v", err)
	}
	defer secondProcessor.Shutdown(context.Background())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if exporter.ExportedCount() >= total {
			remaining, getErr := secondStore.GetAll(context.Background())
			if getErr != nil {
				t.Fatalf("failed reading replayed records: %v", getErr)
			}
			if len(remaining) == 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	remaining, err := secondStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("failed reading final store state: %v", err)
	}
	t.Fatalf("expected replay drain, exported=%d remaining=%d", exporter.ExportedCount(), len(remaining))
}

func TestAuditIntegrationIntegrityAndDeliverySemantics(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("delivery-key")

	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:         exporter,
		ExceptionHandler: &auditlog.DefaultAuditExceptionHandler{},
		ExporterTimeout:  time.Second,
		WaitOnExport:     true,
		DeliveryMode:     auditlog.AuditDeliveryModeSyncDirect,
	})
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("audit-integrity")

	valid := makeValidAuditRecordForTest(t, "valid", hmacKey)
	validResult := logger.EmitWithResult(context.Background(), valid)
	if validResult.StatusCode != 200 || validResult.Status != "delivered" {
		t.Fatalf("expected 200 delivered, got %d %s", validResult.StatusCode, validResult.Status)
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("expected one export after valid record, got %d", exporter.ExportCount())
	}

	invalidHMAC := makeValidAuditRecordForTest(t, "invalid-hmac", hmacKey)
	invalidHMAC.HMAC = "bad-hmac"
	invalidHMACResult := logger.EmitWithResult(context.Background(), invalidHMAC)
	if invalidHMACResult.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid hmac, got %d", invalidHMACResult.StatusCode)
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("export count should remain 1 after invalid hmac, got %d", exporter.ExportCount())
	}
}

func TestAuditIntegrationWaitOnExportClearsFileStore(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("wait-export-file-key")
	storeDir := t.TempDir()
	fileStore, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("file store: %v", err)
	}
	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      fileStore,
		ExceptionHandler:   &auditlog.DefaultAuditExceptionHandler{},
		ScheduleDelay:      10 * time.Second,
		MaxExportBatchSize: 32,
		ExporterTimeout:    5 * time.Second,
		RetryPolicy: auditlog.RetryPolicy{
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		WaitOnExport: true,
		DeliveryMode: auditlog.AuditDeliveryModeAsyncStoreRetry,
	})
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("audit-wait-file")

	const n = 20
	for i := 0; i < n; i++ {
		rec := makeValidAuditRecordForTest(t, fmt.Sprintf("wait-%d", i), hmacKey)
		res := logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != 200 || res.Status != "delivered" {
			t.Fatalf("emit %d: got %d %s %s", i, res.StatusCode, res.Status, res.Reason)
		}
	}
	remaining, err := fileStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected empty file store after delivered emits, got %d", len(remaining))
	}
}

func TestAuditIntegrationWaitOnExportClearsFileStoreAutoSignDuplicateRecordIDAttr(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("wait-export-dup-key")
	storeDir := t.TempDir()
	fileStore, err := auditlog.NewAuditLogFileStore(storeDir)
	if err != nil {
		t.Fatalf("file store: %v", err)
	}
	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      fileStore,
		ExceptionHandler:   &auditlog.DefaultAuditExceptionHandler{},
		ScheduleDelay:      10 * time.Second,
		MaxExportBatchSize: 32,
		ExporterTimeout:    5 * time.Second,
		RetryPolicy: auditlog.RetryPolicy{
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        5 * time.Millisecond,
			BackoffMultiplier: 1.2,
		},
		WaitOnExport: true,
		DeliveryMode: auditlog.AuditDeliveryModeAsyncStoreRetry,
	})
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("audit-wait-dup")

	const n = 20
	for i := 0; i < n; i++ {
		rec := makeAutoSignedAuditRecordDuplicateRecordIDAttr(t, i)
		res := logger.EmitWithResult(context.Background(), rec)
		if res.StatusCode != 200 || res.Status != "delivered" {
			t.Fatalf("emit %d: got %d %s %s", i, res.StatusCode, res.Status, res.Reason)
		}
	}
	remaining, err := fileStore.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected empty file store after delivered emits (duplicate audit.record.id attr), got %d", len(remaining))
	}
}

func TestAuditIntegrationSignatureVerifierSemantics(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("signature-key")

	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:         exporter,
		ExceptionHandler: &auditlog.DefaultAuditExceptionHandler{},
		ExporterTimeout:  time.Second,
		WaitOnExport:     true,
		DeliveryMode:     auditlog.AuditDeliveryModeSyncDirect,
	})
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
		auditlog.WithAuditSignatureVerifier(func(record auditlog.AuditRecord, canonicalPayload []byte) error {
			if record.Signature == "good-signature" {
				return nil
			}
			return errors.New("invalid signature")
		}),
	)
	logger := provider.Logger("audit-signature")

	valid := makeValidAuditRecordForTest(t, "sig-valid", hmacKey)
	valid.Signature = "good-signature"
	validResult := logger.EmitWithResult(context.Background(), valid)
	if validResult.StatusCode != 200 {
		t.Fatalf("expected 200 for valid signature, got %d", validResult.StatusCode)
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("expected one export after valid signature, got %d", exporter.ExportCount())
	}

	invalid := makeValidAuditRecordForTest(t, "sig-invalid", hmacKey)
	invalid.Signature = "bad-signature"
	invalidResult := logger.EmitWithResult(context.Background(), invalid)
	if invalidResult.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid signature, got %d", invalidResult.StatusCode)
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("export count should remain 1 after invalid signature, got %d", exporter.ExportCount())
	}
}

func TestAuditIntegrationProviderRateLimitWithProcessor(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("ratelimit-key")

	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:         exporter,
		ExceptionHandler: &auditlog.DefaultAuditExceptionHandler{},
		ExporterTimeout:  time.Second,
		WaitOnExport:     true,
		DeliveryMode:     auditlog.AuditDeliveryModeSyncDirect,
	})
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
		auditlog.WithAuditMaxRequestsPerSecond(1),
	)
	logger := provider.Logger("audit-rate-limit")

	first := makeValidAuditRecordForTest(t, "rl-1", hmacKey)
	firstResult := logger.EmitWithResult(context.Background(), first)
	if firstResult.StatusCode == 429 {
		t.Fatalf("first request should not be rate limited")
	}

	second := makeValidAuditRecordForTest(t, "rl-2", hmacKey)
	secondResult := logger.EmitWithResult(context.Background(), second)
	if secondResult.StatusCode != 429 {
		t.Fatalf("expected second request status 429, got %d", secondResult.StatusCode)
	}
	if secondResult.RetryAfter <= 0 {
		t.Fatalf("expected retry-after hint for rate-limited request")
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("expected only one exported request after rate-limit, got %d", exporter.ExportCount())
	}
}

func TestAuditIntegrationSyncDirectQueuedWhenNotWaiting(t *testing.T) {
	exporter := &testMockExporter{}
	hmacKey := []byte("queued-key")

	processor, err := auditlog.NewAuditLogProcessor(auditlog.AuditLogProcessorConfig{
		Exporter:         exporter,
		ExceptionHandler: &auditlog.DefaultAuditExceptionHandler{},
		ExporterTimeout:  time.Second,
		WaitOnExport:     false,
		DeliveryMode:     auditlog.AuditDeliveryModeSyncDirect,
	})
	if err != nil {
		t.Fatalf("failed to create processor: %v", err)
	}
	defer processor.Shutdown(context.Background())

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHMACVerificationKey(hmacKey),
		auditlog.WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("audit-sync-queued")

	record := makeValidAuditRecordForTest(t, "sync-queued", hmacKey)
	result := logger.EmitWithResult(context.Background(), record)
	if result.StatusCode != 202 || result.Status != "queued" {
		t.Fatalf("expected queued response 202, got %d %s", result.StatusCode, result.Status)
	}
	if exporter.ExportCount() != 1 {
		t.Fatalf("expected one direct export, got %d", exporter.ExportCount())
	}
}

type canonicalAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type canonicalAuditRecord struct {
	Timestamp     string               `json:"timestamp"`
	Observed      string               `json:"observed_timestamp"`
	EventName     string               `json:"event_name"`
	Actor         string               `json:"actor"`
	ActorType     string               `json:"actor_type"`
	Action        string               `json:"action"`
	TargetID      string               `json:"target_id"`
	TargetType    string               `json:"target_type,omitempty"`
	Outcome       string               `json:"outcome"`
	SourceID      string               `json:"source_id,omitempty"`
	Body          string               `json:"body"`
	Attributes    []canonicalAttribute `json:"attributes"`
	RecordID      string               `json:"record_id"`
	SchemaVersion string               `json:"schema_version"`
	SequenceNo    int64                `json:"sequence_no,omitempty"`
	PrevHash      string               `json:"prev_hash,omitempty"`
}

func makeAutoSignedAuditRecordDuplicateRecordIDAttr(t *testing.T, i int) auditlog.AuditRecord {
	t.Helper()
	now := time.Now().UTC()
	rid := fmt.Sprintf("rec-dup-%d-%d", i, now.UnixNano())
	base := logtest.RecordFactory{
		Timestamp:                 now,
		ObservedTimestamp:         now,
		Severity:                  log.SeverityInfo,
		Body:                      log.StringValue(fmt.Sprintf(`{"event":"user.login","n":%d,"id":%q}`, i, rid)),
		AttributeValueLengthLimit: -1,
		AttributeCountLimit:       -1,
		Attributes: []log.KeyValue{
			log.String("audit.record.id", rid),
			log.String("base", "integration-dup"),
		},
	}.NewRecord()
	return auditlog.AuditRecord{
		Record:        base,
		EventName:     "user.login",
		Actor:         log.StringValue("alice@example.com"),
		ActorType:     "user",
		Action:        "login",
		Resource:      log.StringValue("/api/widgets"),
		Outcome:       "success",
		SourceIP:      "192.0.2.10",
		RecordID:      rid,
		SchemaVersion: "1.0",
		HashAlgorithm: "sha256",
	}
}

func makeValidAuditRecordForTest(t *testing.T, suffix string, hmacKey []byte) auditlog.AuditRecord {
	t.Helper()

	now := time.Now().UTC()
	base := logtest.RecordFactory{
		Timestamp:                 now,
		ObservedTimestamp:         now,
		Severity:                  log.SeverityInfo,
		Body:                      log.StringValue(`{"event":"auth","status":"ok"}`),
		AttributeValueLengthLimit: -1,
		AttributeCountLimit:       -1,
		Attributes:                []log.KeyValue{log.String("base", "value")},
	}.NewRecord()

	record := auditlog.AuditRecord{
		Record:        base,
		EventName:     "user.login",
		Actor:         log.StringValue("actor"),
		ActorType:     "user",
		Action:        "login",
		Resource:      log.StringValue("resource"),
		Outcome:       "success",
		RecordID:      fmt.Sprintf("record-%s", suffix),
		SchemaVersion: "1.0",
		HashAlgorithm: "sha256",
	}

	canonical, err := canonicalizeAuditRecord(record)
	if err != nil {
		t.Fatalf("failed to canonicalize audit record: %v", err)
	}
	mac := hmac.New(sha256.New, hmacKey)
	_, _ = mac.Write(canonical)
	record.HMAC = strings.ToLower(hex.EncodeToString(mac.Sum(nil)))

	return record
}

func canonicalizeAuditRecord(record auditlog.AuditRecord) ([]byte, error) {
	attrs := make([]canonicalAttribute, 0, record.AttributesLen())
	record.WalkAttributes(func(kv log.KeyValue) bool {
		attrs = append(attrs, canonicalAttribute{
			Key:   string(kv.Key),
			Value: kv.Value.String(),
		})
		return true
	})
	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i].Key == attrs[j].Key {
			return attrs[i].Value < attrs[j].Value
		}
		return attrs[i].Key < attrs[j].Key
	})

	targetID := strings.TrimSpace(record.TargetID)
	if targetID == "" && record.Resource.Kind() != log.KindEmpty {
		targetID = strings.TrimSpace(record.Resource.String())
	}
	targetType := strings.TrimSpace(record.TargetType)
	payload := canonicalAuditRecord{
		Timestamp:     record.Timestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		Observed:      record.ObservedTimestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		EventName:     record.EventName,
		Actor:         record.Actor.String(),
		ActorType:     record.ActorType,
		Action:        record.Action,
		TargetID:      targetID,
		TargetType:    targetType,
		Outcome:       record.Outcome,
		SourceID:      record.SourceIP,
		Body:          record.Body().String(),
		Attributes:    attrs,
		RecordID:      record.RecordID,
		SchemaVersion: record.SchemaVersion,
		SequenceNo:    record.SequenceNo,
		PrevHash:      record.PrevHash,
	}
	return json.Marshal(payload)
}
