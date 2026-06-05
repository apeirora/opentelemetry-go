// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

func TestWithAuditRecordSigningHMACBody(t *testing.T) {
	key := []byte("body-sign-key")
	provider := NewAuditLoggerProvider(
		WithAuditRecordSigning(AuditIntegrityHMAC, AuditSignContentBody),
		WithAuditHMACVerificationKey(key),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("body-sign")
	rec := sampleAuditRecord(t)
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	signRec := provider.applyDefaultSignContent(rec)
	want, err := signAuditRecordHMAC(signRec, key, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAuditIntegrity(want, key, nil, "sha256", AuditSignContentBody); err != nil {
		t.Fatal(err)
	}
}

func TestWithAuditRecordSigningHashAndHMACMeta(t *testing.T) {
	key := []byte("hash-hmac-key")
	provider := NewAuditLoggerProvider(
		WithAuditRecordSigning(AuditIntegrityHash|AuditIntegrityHMAC, AuditSignContentMeta),
		WithAuditHMACVerificationKey(key),
		WithAuditHashAlgorithm("sha256"),
	)
	logger := provider.Logger("hash-hmac")
	rec := sampleAuditRecord(t)
	res := logger.EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	enriched, err := provider.enrichIntegrity(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMACIntegrity(enriched) {
		t.Fatal("expected HMAC integrity when hash and HMAC signing are configured")
	}
}

func TestWithAuditRecordSigningAttrPayload(t *testing.T) {
	key := []byte("attr-sign-key")
	rec := sampleAuditRecord(t)
	provider := NewAuditLoggerProvider(
		WithAuditRecordSigning(AuditIntegrityHMAC, AuditSignContentAttr),
		WithAuditHMACVerificationKey(key),
		WithAuditHashAlgorithm("sha256"),
	)
	res := provider.Logger("attr-sign").EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
	rec.SignContent = string(AuditSignContentAttr)
	signed, err := signAuditRecordHMAC(rec, key, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAuditIntegrity(signed, key, nil, "sha256", AuditSignContentAttr); err != nil {
		t.Fatal(err)
	}
}

func TestCertificateSignatureSignerBodyContent(t *testing.T) {
	signer, verifier, err := testCertSignerAndVerifier(t)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewAuditLoggerProvider(
		WithAuditRecordSigning(AuditIntegritySignature, AuditSignContentBody),
		WithAuditSignatureSigner(signer),
		WithAuditSignatureVerifier(verifier),
	)
	rec := sampleAuditRecord(t)
	res := provider.Logger("cert-sign-body").EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
}

func TestCertificateSignatureSigner(t *testing.T) {
	signer, verifier, err := testCertSignerAndVerifier(t)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewAuditLoggerProvider(
		WithAuditRecordSigning(AuditIntegritySignature, AuditSignContentMeta),
		WithAuditSignatureSigner(signer),
		WithAuditSignatureVerifier(verifier),
	)
	rec := sampleAuditRecord(t)
	res := provider.Logger("cert-sign").EmitWithResult(context.Background(), rec)
	if res.StatusCode != 202 {
		t.Fatalf("expected 202, got %d %s", res.StatusCode, res.Reason)
	}
}

func sampleAuditRecord(t *testing.T) AuditRecord {
	t.Helper()
	base := Record{}
	now := time.Now().UTC()
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetBody(log.StringValue(`{"msg":"hello"}`))
	base.AddAttributes(log.String("base", "v"))
	return AuditRecord{
		Record:        base,
		EventName:     "user.action",
		Actor:         log.StringValue("actor"),
		ActorType:     "user",
		Action:        "READ",
		Resource:      log.StringValue("/r"),
		Outcome:       "success",
		RecordID:      testAuditRecordID(5),
		SchemaVersion: "1.0",
	}
}

func testCertSignerAndVerifier(t *testing.T) (AuditSignatureSigner, AuditSignatureVerifier, error) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	signer, err := NewAuditCertificateSignatureSigner(certPEM, keyPEM)
	if err != nil {
		return nil, nil, err
	}
	verifier, err := NewAuditCertificateSignatureVerifier(certPEM)
	if err != nil {
		return nil, nil, err
	}
	return signer, verifier, nil
}
