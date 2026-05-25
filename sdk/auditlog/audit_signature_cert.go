// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
)

type AuditSignatureSigner func(record AuditRecord, payload []byte) (string, error)

func NewAuditCertificateSignatureSigner(certPEM, keyPEM []byte) (AuditSignatureSigner, error) {
	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	_ = certPEM
	return func(_ AuditRecord, payload []byte) (string, error) {
		sum := sha256.Sum256(payload)
		var sig []byte
		switch k := key.(type) {
		case *rsa.PrivateKey:
			sig, err = rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
		case *ecdsa.PrivateKey:
			sig, err = ecdsa.SignASN1(rand.Reader, k, sum[:])
		default:
			return "", fmt.Errorf("auditlog: unsupported private key type %T", key)
		}
		if err != nil {
			return "", fmt.Errorf("auditlog: certificate sign: %w", err)
		}
		return base64.StdEncoding.EncodeToString(sig), nil
	}, nil
}

func NewAuditCertificateSignatureSignerFromFiles(certPath, keyPath string) (AuditSignatureSigner, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("auditlog: read certificate %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("auditlog: read private key %q: %w", keyPath, err)
	}
	return NewAuditCertificateSignatureSigner(certPEM, keyPEM)
}

func parsePrivateKeyPEM(pemBytes []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("auditlog: no PEM block in private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("auditlog: unrecognized private key PEM")
}
