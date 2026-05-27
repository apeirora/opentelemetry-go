// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestCertificateSignerVerifierFromFiles(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "auditlog-test"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	signer, err := NewAuditCertificateSignatureSignerFromFiles(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAuditCertificateSignatureVerifierFromFiles(certPath)
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"k":"v"}`)
	sig, err := signer(AuditRecord{}, payload)
	if err != nil {
		t.Fatal(err)
	}
	rec := AuditRecord{Signature: sig}
	if err := verifier(rec, payload); err != nil {
		t.Fatalf("expected signature verification to pass: %v", err)
	}
	if err := verifier(rec, []byte(`{"k":"tampered"}`)); err == nil {
		t.Fatal("expected signature verification to fail for tampered payload")
	}
}

func TestCertificateSignerVerifierFromFilesErrors(t *testing.T) {
	tmp := t.TempDir()
	_, err := NewAuditCertificateSignatureSignerFromFiles(filepath.Join(tmp, "missing-cert.pem"), filepath.Join(tmp, "missing-key.pem"))
	if err == nil {
		t.Fatal("expected signer constructor to fail when cert file is missing")
	}

	_, err = NewAuditCertificateSignatureVerifierFromFiles(filepath.Join(tmp, "missing-cert.pem"))
	if err == nil {
		t.Fatal("expected verifier constructor to fail when cert file is missing")
	}
}

func TestParseCertificatePEMErrors(t *testing.T) {
	if _, err := parseCertificatePEM([]byte("not-a-pem")); err == nil {
		t.Fatal("expected parseCertificatePEM to fail for invalid PEM data")
	}
	if _, err := parseCertificatePEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid-der")})); err == nil {
		t.Fatal("expected parseCertificatePEM to fail for invalid certificate DER")
	}
}

func TestParsePrivateKeyPEMErrors(t *testing.T) {
	if _, err := parsePrivateKeyPEM([]byte("not-a-pem")); err == nil {
		t.Fatal("expected parsePrivateKeyPEM to fail for invalid PEM data")
	}
	if _, err := parsePrivateKeyPEM(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("invalid-der")})); err == nil {
		t.Fatal("expected parsePrivateKeyPEM to fail for unknown private key DER")
	}
}

func TestCertificateVerifierRejectsInvalidBase64Signature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(8),
		Subject:      pkix.Name{CommonName: "auditlog-test"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	verifier, err := NewAuditCertificateSignatureVerifier(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	err = verifier(AuditRecord{Signature: "***invalid***"}, []byte("payload"))
	if err == nil {
		t.Fatal("expected verifier to fail on non-base64 signature")
	}
}
