// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

func TestWithAuditHMACVerificationKeyFromEnvironment_Variable(t *testing.T) {
	t.Setenv(EnvAuditlogHMACKeyFile, "")
	t.Setenv(EnvAuditlogHMACKey, "env-hmac-key")

	key := []byte("env-hmac-key")
	provider := NewAuditLoggerProvider(
		WithAuditHMACVerificationKeyFromEnvironment(),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("env-key")
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
		RecordID:      "rid-env-1",
		SchemaVersion: "1.0",
	}
	wantSigned, err := signAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	if res.Hash != "" {
		t.Fatalf("expected empty hash in result, got %q", res.Hash)
	}
	if wantSigned.HMAC == "" {
		t.Fatal("expected hmac on signed record")
	}
}

func TestWithAuditHMACVerificationKeyFromEnvironment_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(path, []byte("file-hmac-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAuditlogHMACKeyFile, path)
	t.Setenv(EnvAuditlogHMACKey, "ignored-when-file-set")

	key := []byte("file-hmac-key")
	provider := NewAuditLoggerProvider(
		WithAuditHMACVerificationKeyFromEnvironment(),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("file-key")
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
		RecordID:      "rid-file-1",
		SchemaVersion: "1.0",
	}
	wantSigned, err := signAuditRecordHMAC(rec, key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	if res.Hash != "" {
		t.Fatalf("expected empty hash in result, got %q", res.Hash)
	}
	if wantSigned.HMAC == "" {
		t.Fatal("expected hmac on signed record")
	}
}

func TestHMACVerificationKeyFromEnvironment_FileReadError(t *testing.T) {
	t.Setenv(EnvAuditlogHMACKeyFile, filepath.Join(t.TempDir(), "missing"))
	t.Setenv(EnvAuditlogHMACKey, "")

	if _, err := HMACVerificationKeyFromEnvironment(); err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestHMACVerificationKeyFromEnvironment_FileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("  \n  "), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAuditlogHMACKeyFile, path)
	t.Setenv(EnvAuditlogHMACKey, "")

	if _, err := HMACVerificationKeyFromEnvironment(); err == nil {
		t.Fatal("expected error for empty key file")
	}
}

func TestWithAuditHMACVerificationKeyFromEnvironment_UnsetNoOp(t *testing.T) {
	t.Setenv(EnvAuditlogHMACKeyFile, "")
	t.Setenv(EnvAuditlogHMACKey, "")

	explicit := []byte("explicit")
	p := NewAuditLoggerProvider(
		WithAuditHMACVerificationKey(explicit),
		WithAuditHMACVerificationKeyFromEnvironment(),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := p.Logger("noop-env")
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
		RecordID:      "rid-noop-1",
		SchemaVersion: "1.0",
	}
	wantSigned, err := signAuditRecordHMAC(rec, explicit, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	if res.Hash != "" {
		t.Fatalf("expected empty hash in result, got %q", res.Hash)
	}
	if wantSigned.HMAC == "" {
		t.Fatal("expected hmac on signed record")
	}
}
