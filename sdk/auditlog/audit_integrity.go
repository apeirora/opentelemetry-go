// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhash "hash"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/log"
)

type canonicalAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type canonicalAuditRecord struct {
	Timestamp     string               `json:"timestamp"`
	Observed      string               `json:"observed_timestamp"`
	EventName     string               `json:"event_name"`
	Actor         string               `json:"actor"`
	ActorType     string               `json:"actor_type"`
	Action        string               `json:"action"`
	Resource      string               `json:"resource"`
	Outcome       string               `json:"outcome"`
	SourceIP      string               `json:"source_ip,omitempty"`
	Body          string               `json:"body"`
	Attributes    []canonicalAttribute `json:"attributes"`
	RecordID      string               `json:"record_id"`
	SchemaVersion string               `json:"schema_version"`
	SequenceNo    int64                `json:"sequence_no,omitempty"`
	PrevHash      string               `json:"prev_hash,omitempty"`
}

func signAuditRecordHMAC(record AuditRecord, key []byte, configuredHashAlgorithm string) (AuditRecord, error) {
	if len(key) == 0 {
		return AuditRecord{}, fmt.Errorf("hmac key is required for signing")
	}
	alg := resolveHashAlgorithm(record, configuredHashAlgorithm)
	canonical, err := canonicalizeAuditRecord(record)
	if err != nil {
		return AuditRecord{}, err
	}
	hashHex, err := computeHashHex(alg, canonical)
	if err != nil {
		return AuditRecord{}, err
	}
	h, err := hmacHasherForAlgorithm(alg)
	if err != nil {
		return AuditRecord{}, err
	}
	mac := hmac.New(h, key)
	_, _ = mac.Write(canonical)
	record.Hash = hashHex
	record.HMAC = strings.ToLower(hex.EncodeToString(mac.Sum(nil)))
	return record, nil
}

func verifyAuditIntegrity(
	record AuditRecord,
	hmacKey []byte,
	signatureVerifier AuditSignatureVerifier,
	configuredHashAlgorithm string,
) error {
	canonical, err := canonicalizeAuditRecord(record)
	if err != nil {
		return err
	}
	alg := resolveHashAlgorithm(record, configuredHashAlgorithm)
	expected, err := computeHashHex(alg, canonical)
	if err != nil {
		return newAuditStatusError(AuditErrorInvalidRequest, "invalid hash configuration", false, err)
	}
	if !strings.EqualFold(record.Hash, expected) {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit hash verification failed", false, nil)
	}
	if record.HMAC != "" {
		if len(hmacKey) == 0 {
			return newAuditStatusError(AuditErrorInvalidRequest, "audit hmac present but no verification key configured", false, nil)
		}
		h, err := hmacHasherForAlgorithm(alg)
		if err != nil {
			return newAuditStatusError(AuditErrorInvalidRequest, "invalid hmac algorithm", false, err)
		}
		mac := hmac.New(h, hmacKey)
		_, _ = mac.Write(canonical)
		macHex := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(strings.ToLower(macHex)), []byte(strings.ToLower(record.HMAC))) {
			return newAuditStatusError(AuditErrorInvalidRequest, "audit hmac verification failed", false, nil)
		}
	}
	if record.Signature != "" {
		if signatureVerifier == nil {
			return newAuditStatusError(AuditErrorInvalidRequest, "audit signature present but no signature verifier configured", false, nil)
		}
		if err := signatureVerifier(record, canonical); err != nil {
			return newAuditStatusError(AuditErrorInvalidRequest, "audit signature verification failed", false, err)
		}
	}
	return nil
}

func resolveHashAlgorithm(record AuditRecord, configured string) string {
	if configured != "" {
		return normalizeHashAlgorithm(configured)
	}
	if bodyAlg := hashAlgorithmFromBody(record.Body()); bodyAlg != "" {
		return normalizeHashAlgorithm(bodyAlg)
	}
	if record.HashAlgorithm != "" {
		return normalizeHashAlgorithm(record.HashAlgorithm)
	}
	return "sha256"
}

func normalizeHashAlgorithm(algorithm string) string {
	return strings.ToLower(strings.TrimSpace(algorithm))
}

func hashAlgorithmFromBody(body log.Value) string {
	raw := strings.TrimSpace(body.String())
	if raw == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	keys := []string{"hash_algorithm", "hashAlgorithm", "algorithm"}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func computeHashHex(algorithm string, canonical []byte) (string, error) {
	switch algorithm {
	case "sha256":
		sum := sha256.Sum256(canonical)
		return hex.EncodeToString(sum[:]), nil
	case "sha512":
		sum := sha512.Sum512(canonical)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}
}

func hmacHasherForAlgorithm(algorithm string) (func() stdhash.Hash, error) {
	switch algorithm {
	case "sha256":
		return sha256.New, nil
	case "sha512":
		return sha512.New, nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm for hmac: %s", algorithm)
	}
}

func canonicalizeAuditRecord(record AuditRecord) ([]byte, error) {
	attrs := make([]canonicalAttribute, 0, record.AttributesLen())
	record.WalkAttributes(func(kv log.KeyValue) bool {
		attrs = append(attrs, canonicalAttribute{
			Key:   string(kv.Key),
			Value: kv.Value.String(),
		})
		return true
	})
	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i].Key == attrs[j].Key {
			return attrs[i].Value < attrs[j].Value
		}
		return attrs[i].Key < attrs[j].Key
	})
	payload := canonicalAuditRecord{
		Timestamp:     record.Timestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		Observed:      record.ObservedTimestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		EventName:     record.EventName,
		Actor:         record.Actor.String(),
		ActorType:     record.ActorType,
		Action:        record.Action,
		Resource:      record.Resource.String(),
		Outcome:       record.Outcome,
		SourceIP:      record.SourceIP,
		Body:          record.Body().String(),
		Attributes:    attrs,
		RecordID:      record.RecordID,
		SchemaVersion: record.SchemaVersion,
		SequenceNo:    record.SequenceNo,
		PrevHash:      record.PrevHash,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize audit record: %w", err)
	}
	return data, nil
}
