package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
)

func main() {
	outDir := filepath.Join("..", "..")
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "auditlog-testapp-dev"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certPath := filepath.Join(outDir, "dev_sign_cert.pem")
	keyPath := filepath.Join(outDir, "dev_sign_key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s and %s\n", certPath, keyPath)
}
