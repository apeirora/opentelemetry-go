// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/deszhou/jcs"
	"go.opentelemetry.io/otel/log"
)

const (
	auditAttrIntegrityValue       = "audit.integrity.value"
	auditAttrIntegrityAlgorithm   = "audit.integrity.algorithm"
	auditAttrIntegrityCertificate = "audit.integrity.certificate"
	auditAttrSourceType           = "audit.source.type"
)

func isIntegrityAttributeKey(key string) bool {
	switch key {
	case auditAttrIntegrityValue, auditAttrIntegrityAlgorithm, auditAttrIntegrityCertificate,
		auditAttrHMAC, auditAttrHash, auditAttrSignature:
		return true
	default:
		return strings.HasPrefix(key, "audit.integrity.")
	}
}

func jcsSigningPayload(record AuditRecord) ([]byte, error) {
	switch signContentMode(record, "") {
	case AuditSignContentBody:
		body := record.Body().String()
		wrapped, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			return nil, err
		}
		return jcs.Transform(wrapped)
	case AuditSignContentAttr:
		return jcsCanonicalAttributes(record)
	default:
		return jcsCanonicalAuditRecord(record)
	}
}

func jcsCanonicalAttributes(record AuditRecord) ([]byte, error) {
	attrs := collectNonIntegrityAttributes(record)
	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i].Key == attrs[j].Key {
			return attrs[i].Value < attrs[j].Value
		}
		return attrs[i].Key < attrs[j].Key
	})
	data, err := json.Marshal(attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal audit attributes: %w", err)
	}
	return jcs.Transform(data)
}

func jcsCanonicalAuditRecord(record AuditRecord) ([]byte, error) {
	targetID, targetType := auditTargetFields(record)
	attrs := collectNonIntegrityAttributes(record)
	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i].Key == attrs[j].Key {
			return attrs[i].Value < attrs[j].Value
		}
		return attrs[i].Key < attrs[j].Key
	})
	payload := map[string]any{
		"timestamp":          record.Timestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		"observed_timestamp": record.ObservedTimestamp().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		"event_name":         record.EventName,
		"audit.record.id":    record.RecordID,
		"audit.actor.id":     record.Actor.String(),
		"audit.actor.type":   record.ActorType,
		"audit.action":       record.Action,
		"audit.outcome":      record.Outcome,
		"attributes":         attrs,
	}
	if targetID != "" {
		payload["audit.target.id"] = targetID
	}
	if targetType != "" {
		payload["audit.target.type"] = targetType
	}
	if record.SourceIP != "" {
		payload["audit.source.id"] = record.SourceIP
	}
	if record.SourceType != "" {
		payload["audit.source.type"] = record.SourceType
	}
	if body := record.Body().String(); body != "" {
		payload["body"] = body
	}
	if record.SchemaVersion != "" {
		payload["audit.schema.version"] = record.SchemaVersion
	}
	if record.SequenceNo > 0 {
		payload["audit.sequence.number"] = record.SequenceNo
	}
	if record.PrevHash != "" {
		payload["audit.prev.hash"] = record.PrevHash
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal audit record: %w", err)
	}
	return jcs.Transform(data)
}

type jcsAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func collectNonIntegrityAttributes(record AuditRecord) []jcsAttr {
	attrs := make([]jcsAttr, 0, record.AttributesLen())
	record.WalkAttributes(func(kv log.KeyValue) bool {
		key := string(kv.Key)
		if isIntegrityAttributeKey(key) || key == auditAttrSignContent {
			return true
		}
		attrs = append(attrs, jcsAttr{Key: key, Value: kv.Value.String()})
		return true
	})
	return attrs
}

func hmacIntegrityAlgorithm(alg string) string {
	switch normalizeHashAlgorithm(alg) {
	case "sha512":
		return "HMAC-SHA512"
	default:
		return "HMAC-SHA256"
	}
}

func encodeIntegrityValue(mac []byte) string {
	return base64.StdEncoding.EncodeToString(mac)
}

func decodeIntegrityValueHMAC(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty integrity value")
	}
	if dec, err := base64.StdEncoding.DecodeString(value); err == nil {
		return dec, nil
	}
	if raw, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(value))); err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("invalid audit.integrity.value encoding")
}

func sha256Hex(payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
