package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	body = `{"event":"user.login","n":0,"id":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"}`
)

type attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func hmacHex(key, payload []byte) string {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

func sortedAttrs(m map[string]string) []attr {
	out := make([]attr, 0, len(m))
	for k, v := range m {
		out = append(out, attr{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key == out[j].Key {
			return out[i].Value < out[j].Value
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func main() {
	pemBytes, err := os.ReadFile("../../dev_hmac_key.txt")
	if err != nil {
		panic(err)
	}
	pemStr := strings.TrimSpace(string(pemBytes))

	keys := map[string][]byte{
		"pem_trimmed":     []byte(pemStr),
		"pem_raw_file":    pemBytes,
	}
	if block, _ := pem.Decode([]byte(pemStr)); block != nil {
		keys["pem_der"] = block.Bytes
		if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			_ = k
		}
		if rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			keys["pkcs1_der"] = block.Bytes
			_ = rsaKey
		}
	}

	ts, _ := time.Parse(time.RFC3339Nano, "2026-05-19T12:36:04.2396044Z")
	tsStr := ts.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")

	otlp := map[string]string{
		"audit.record_id":      "rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a",
		"base":                 "testapp",
		"audit.actor":          "alice@example.com",
		"audit.actor_type":     "user",
		"audit.action":         "login",
		"audit.resource":       "/api/widgets",
		"audit.outcome":        "success",
		"audit.schema_version": "1.0",
		"audit.source_ip":      "192.0.2.10",
		"sign_content":         "meta",
	}

	canonical := map[string]any{
		"timestamp":          tsStr,
		"observed_timestamp": tsStr,
		"event_name":         "user.login",
		"actor":              otlp["audit.actor"],
		"actor_type":         otlp["audit.actor_type"],
		"action":             otlp["audit.action"],
		"resource":           otlp["audit.resource"],
		"outcome":            otlp["audit.outcome"],
		"source_ip":          otlp["audit.source_ip"],
		"body":               body,
		"attributes":         sortedAttrs(otlp),
		"record_id":          otlp["audit.record_id"],
		"schema_version":     otlp["audit.schema_version"],
	}
	payload, _ := json.Marshal(canonical)

	sdkAttrs := sortedAttrs(map[string]string{
		"audit.record_id": otlp["audit.record_id"],
		"base":            otlp["base"],
		"sign_content":    otlp["sign_content"],
	})
	canonical["attributes"] = sdkAttrs
	sdkPayload, _ := json.Marshal(canonical)

	for name, key := range keys {
		fmt.Printf("%s collector_style_hex=%s b64=%s\n", name, hmacHex(key, payload), b64(hmacHex(key, payload)))
		fmt.Printf("%s sdk_style_hex=%s b64=%s\n", name, hmacHex(key, sdkPayload), b64(hmacHex(key, sdkPayload)))
	}
}

func b64(hexStr string) string {
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return err.Error()
	}
	return base64.StdEncoding.EncodeToString(b)
}
