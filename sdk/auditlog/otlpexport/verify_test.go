// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	"go.opentelemetry.io/otel/sdk/auditlog/otlpexport"
)

func TestVerifyStartupTLSRejectsWrongCA(t *testing.T) {
	t.Parallel()

	srv, srvAddr, _ := startTLSTestServer(t)
	defer srv.Close()

	_, _, wrongPool := mustGenerateCA(t, "wrong-ca")

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(srvAddr),
		otlpexport.WithTLSClientConfig(&tls.Config{
			RootCAs:    wrongPool,
			MinVersion: tls.VersionTLS12,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exp.(auditlog.StartupExporterVerifier).VerifyStartup(ctx); err == nil {
		t.Fatal("expected tls verification failure with wrong CA")
	}
}

func TestVerifyStartupTLSAcceptsMatchingCA(t *testing.T) {
	t.Parallel()

	srv, srvAddr, srvPool := startTLSTestServer(t)
	defer srv.Close()

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(srvAddr),
		otlpexport.WithTLSClientConfig(&tls.Config{
			RootCAs:    srvPool,
			MinVersion: tls.VersionTLS12,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exp.(auditlog.StartupExporterVerifier).VerifyStartup(ctx); err != nil {
		t.Fatalf("expected tls verification success: %v", err)
	}
}

func TestVerifyStartupSkipsWhenCollectorUnreachable(t *testing.T) {
	t.Parallel()

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint("127.0.0.1:1"),
		otlpexport.WithTLSClientConfig(&tls.Config{InsecureSkipVerify: true}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = exp.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exp.(auditlog.StartupExporterVerifier).VerifyStartup(ctx); err != nil {
		t.Fatalf("unreachable collector should not fail tls startup verify: %v", err)
	}
}

func TestBuilderFailsOnBadTLS(t *testing.T) {
	t.Parallel()

	srv, srvAddr, _ := startTLSTestServer(t)
	defer srv.Close()

	_, _, wrongPool := mustGenerateCA(t, "wrong-ca")

	exp, err := otlpexport.NewHTTP(
		context.Background(),
		otlpexport.WithEndpoint(srvAddr),
		otlpexport.WithTLSClientConfig(&tls.Config{
			RootCAs:    wrongPool,
			MinVersion: tls.VersionTLS12,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	builder, err := auditlog.NewAuditLogProcessorBuilder(exp, auditlog.NewAuditLogInMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(); err == nil {
		t.Fatal("expected processor build to fail on bad tls")
	}
}

func startTLSTestServer(t *testing.T) (*tlsTestServer, string, *x509.CertPool) {
	t.Helper()

	caCert, caKey, caPool := mustGenerateCA(t, "startup-verify-ca")
	serverCert := mustGenerateServerCert(t, caCert, caKey, "127.0.0.1")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}
	srv := &tlsTestServer{listener: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			tlsConn := tls.Server(conn, cfg)
			_ = tlsConn.Handshake()
			_ = tlsConn.Close()
		}
	}()
	return srv, ln.Addr().String(), caPool
}

type tlsTestServer struct {
	listener net.Listener
}

func (s *tlsTestServer) Close() {
	_ = s.listener.Close()
}

func mustGenerateCA(t *testing.T, name string) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return cert, caKey, pool
}

func mustGenerateServerCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, host string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP(host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, key.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
