package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/deszhou/jcs"
)

const (
	body     = `{"event":"user.login","n":0,"id":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"}`
	recordID = "rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"
)

func hmacHex(key, payload []byte) string {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

func main() {
	keyBytes, err := os.ReadFile("../../dev_hmac_key.txt")
	if err != nil {
		panic(err)
	}
	key := []byte(strings.TrimSpace(string(keyBytes)))

	ts, _ := time.Parse(time.RFC3339Nano, "2026-05-19T12:36:04.2396044Z")
	tsNano := strconv.FormatInt(ts.UnixNano(), 10)

	attrs := map[string]any{
		"audit.record.id":      map[string]any{"stringValue": recordID},
		"base":                 map[string]any{"stringValue": "testapp"},
		"audit.actor.id":       map[string]any{"stringValue": "alice@example.com"},
		"audit.actor.type":     map[string]any{"stringValue": "user"},
		"audit.action":         map[string]any{"stringValue": "LOGIN"},
		"audit.target.id":      map[string]any{"stringValue": "/api/widgets"},
		"audit.outcome":        map[string]any{"stringValue": "success"},
		"audit.schema.version": map[string]any{"stringValue": "1.0"},
		"audit.source.id":      map[string]any{"stringValue": "192.0.2.10"},
	}

	meta := map[string]any{
		"event_name":         "user.login",
		"body":               map[string]any{"stringValue": body},
		"timestamp":          tsNano,
		"observed_timestamp": tsNano,
		"attributes":         attrs,
	}
	raw, _ := json.Marshal(meta)
	metaPayload, _ := jcs.Transform(raw)

	bodyOnly := map[string]any{
		"body": map[string]any{"stringValue": body},
	}
	bodyRaw, _ := json.Marshal(bodyOnly)
	bodyPayload, _ := jcs.Transform(bodyRaw)

	fmt.Printf("meta_hex=%s b64=%s\n", hmacHex(key, metaPayload), base64.StdEncoding.EncodeToString(mustDecodeHex(hmacHex(key, metaPayload))))
	fmt.Printf("body_hex=%s b64=%s\n", hmacHex(key, bodyPayload), base64.StdEncoding.EncodeToString(mustDecodeHex(hmacHex(key, bodyPayload))))
	fmt.Printf("meta_payload=%s\n", metaPayload)
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
