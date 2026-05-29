// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

const (
	exampleRecordID  = "rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"
	exampleTimestamp = "2026-05-19T12:36:04.2396044Z"
	exampleBody      = `{"event":"user.login","n":0,"id":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"}`

	wantMetaSignHMACHex = "b7a3acf3ba4914fcd7ff8a737c3300b75a038fbe1a2686cc1c28991104dabe4b"
	wantBodySignHMACHex = "7f1fcaf6ff52d6f8f16ab6bca11bea1a71b540ff39ac810a1d3f1446edbae835"
)

func loadTestappHMACKey(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testapp", "dev_hmac_key.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		t.Fatal("dev_hmac_key.txt is empty")
	}
	return []byte(key)
}

func exampleRecordForHMAC() AuditRecord {
	ts, _ := time.Parse(time.RFC3339Nano, exampleTimestamp)
	rid := exampleRecordID
	base := Record{}
	base.SetTimestamp(ts)
	base.SetObservedTimestamp(ts)
	base.SetBody(log.StringValue(exampleBody))
	base.AddAttributes(
		log.String("audit.record.id", rid),
		log.String("base", "testapp"),
		log.String("sign_content", "meta"),
	)
	return AuditRecord{
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
		SignContent:   "meta",
	}
}

func exampleRecordForBodyHMAC() AuditRecord {
	rec := exampleRecordForHMAC()
	rec.SignContent = "body"
	return rec
}

func TestOTLPAuditLoginHMACBodyWithDevKeyFile(t *testing.T) {
	key := loadTestappHMACKey(t)
	signed, err := signAuditRecordHMAC(exampleRecordForBodyHMAC(), key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	if signed.HMAC != wantBodySignHMACHex {
		t.Fatalf("body sign hmac: got %s want %s", signed.HMAC, wantBodySignHMACHex)
	}
}

func TestOTLPAuditLoginHMACWithDevKeyFile(t *testing.T) {
	key := loadTestappHMACKey(t)
	signed, err := signAuditRecordHMAC(exampleRecordForHMAC(), key, "sha256", true)
	if err != nil {
		t.Fatal(err)
	}
	if signed.HMAC != wantMetaSignHMACHex {
		t.Fatalf("meta sign hmac: got %s want %s", signed.HMAC, wantMetaSignHMACHex)
	}
}
