// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

func TestEmitWithResultAutoHMACSigning(t *testing.T) {
	key := []byte("auto-sign-key")
	provider := NewAuditLoggerProvider(
		WithAuditHMACVerificationKey(key),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("auto-sign")
	base := Record{}
	now := time.Now().UTC()
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetBody(log.StringValue(`{"msg":"hello"}`))
	base.AddAttributes(log.String("base", "v"))
	rec := AuditRecord{
		Record:        base,
		EventName:     "user.action",
		Actor:         log.StringValue("actor"),
		ActorType:     "user",
		Action:        "read",
		Resource:      log.StringValue("/r"),
		Outcome:       "success",
		RecordID:      "rid-auto-1",
		SchemaVersion: "1.0",
	}
	wantSigned, err := signAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202 without processors, got %d %s", res.StatusCode, res.Reason)
	}
	if res.Hash != "" {
		t.Fatalf("expected empty hash in result, got %q", res.Hash)
	}
	if wantSigned.Hash != "" {
		t.Fatalf("expected empty hash on signed record, got %q", wantSigned.Hash)
	}
	if wantSigned.HMAC == "" {
		t.Fatal("expected hmac on signed record")
	}
	if err := verifyAuditIntegrity(wantSigned, key, nil, "sha256", AuditSignContentMeta); err != nil {
		t.Fatal(err)
	}
}
