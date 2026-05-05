// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

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
	wantSigned, err := signAuditRecordHMAC(rec, key, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202 without processors, got %d %s", res.StatusCode, res.Reason)
	}
	if res.Hash != wantSigned.Hash {
		t.Fatalf("auto-sign hash %q want %q", res.Hash, wantSigned.Hash)
	}
	if err := verifyAuditIntegrity(wantSigned, key, nil, "sha256"); err != nil {
		t.Fatal(err)
	}
}
