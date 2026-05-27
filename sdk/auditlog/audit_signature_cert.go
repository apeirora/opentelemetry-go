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

func NewAuditCertificateSignatureVerifier(certPEM []byte) (AuditSignatureVerifier, error) {
	cert, err := parseCertificatePEM(certPEM)
	if err != nil {
		return nil, err
	}
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return func(record AuditRecord, payload []byte) error {
			sigBytes, err := base64.StdEncoding.DecodeString(record.Signature)
			if err != nil {
				return fmt.Errorf("auditlog: decode signature: %w", err)
			}
			sum := sha256.Sum256(payload)
			if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sigBytes); err != nil {
				return fmt.Errorf("auditlog: signature verification failed: %w", err)
			}
			return nil
		}, nil
	case *ecdsa.PublicKey:
		return func(record AuditRecord, payload []byte) error {
			sigBytes, err := base64.StdEncoding.DecodeString(record.Signature)
			if err != nil {
				return fmt.Errorf("auditlog: decode signature: %w", err)
			}
			sum := sha256.Sum256(payload)
			if !ecdsa.VerifyASN1(pub, sum[:], sigBytes) {
				return fmt.Errorf("auditlog: signature verification failed")
			}
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("auditlog: unsupported certificate public key type %T", cert.PublicKey)
	}
}

func NewAuditCertificateSignatureVerifierFromFiles(certPath string) (AuditSignatureVerifier, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("auditlog: read certificate %q: %w", certPath, err)
	}
	return NewAuditCertificateSignatureVerifier(certPEM)
}

func parseCertificatePEM(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("auditlog: no PEM block in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auditlog: parse certificate: %w", err)
	}
	return cert, nil
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
