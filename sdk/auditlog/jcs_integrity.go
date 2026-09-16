// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/deszhou/jcs"
	"go.opentelemetry.io/otel/log"
)

const (
	auditAttrIntegrityValue       = "audit.integrity.value"
	auditAttrIntegrityAlgorithm   = "audit.integrity.algorithm"
	auditAttrIntegrityCertificate = "audit.integrity.certificate"
	auditAttrSourceType           = "audit.source.type"

	jsonMaxDepth      = 128
	jsonMaxInputBytes = 1 << 21
)

func isIntegrityAttributeKey(key string) bool {
	switch key {
	case auditAttrIntegrityValue, auditAttrIntegrityAlgorithm, auditAttrIntegrityCertificate:
		return true
	default:
		return strings.HasPrefix(key, "audit.integrity.")
	}
}

func jcsSigningPayload(record AuditRecord) ([]byte, error) {
	switch signContentMode(record, "") {
	case AuditSignContentBody:
		return jcsCanonicalBody(record)
	case AuditSignContentAttr:
		return jcsCanonicalAttributes(record)
	default:
		return jcsCanonicalAuditRecord(record)
	}
}

func jcsCanonicalBody(record AuditRecord) ([]byte, error) {
	body := record.Body()
	if body.Kind() == log.KindEmpty {
		return marshalJCS(map[string]any{})
	}
	typed, err := valueToInterface(body, 0)
	if err != nil {
		return nil, fmt.Errorf("log record body: %w", err)
	}
	return marshalJCS(map[string]any{"body": typed})
}

func jcsCanonicalAttributes(record AuditRecord) ([]byte, error) {
	attrs, err := collectTypedAttributes(record, false)
	if err != nil {
		return nil, err
	}
	if len(attrs) == 0 {
		return marshalJCS(map[string]any{})
	}
	return marshalJCS(attrs)
}

func jcsCanonicalAuditRecord(record AuditRecord) ([]byte, error) {
	data := make(map[string]any)

	if record.EventName != "" {
		data["event_name"] = record.EventName
	}

	if body := record.Body(); body.Kind() != log.KindEmpty {
		typed, err := valueToInterface(body, 0)
		if err != nil {
			return nil, fmt.Errorf("log record body: %w", err)
		}
		data["body"] = typed
	}

	if ts := record.Timestamp(); !ts.IsZero() {
		data["timestamp"] = strconv.FormatInt(ts.UnixNano(), 10)
	}
	if ots := record.ObservedTimestamp(); !ots.IsZero() {
		data["observed_timestamp"] = strconv.FormatInt(ots.UnixNano(), 10)
	}

	if tid := record.TraceID(); tid.IsValid() {
		data["trace_id"] = tid.String()
	}
	if sid := record.SpanID(); sid.IsValid() {
		data["span_id"] = sid.String()
	}

	attrs, err := collectTypedAttributes(record, true)
	if err != nil {
		return nil, err
	}
	if len(attrs) > 0 {
		data["attributes"] = attrs
	}

	return marshalJCS(data)
}

func marshalJCS(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > jsonMaxInputBytes {
		return nil, fmt.Errorf("serialized log record exceeds size limit (%d > %d bytes)", len(raw), jsonMaxInputBytes)
	}
	return jcs.Transform(raw)
}

func collectTypedAttributes(record AuditRecord, mergeAudit bool) (map[string]any, error) {
	attrs := make(map[string]any)
	var walkErr error
	record.WalkAttributes(func(kv log.KeyValue) bool {
		key := string(kv.Key)
		if isIntegrityAttributeKey(key) || key == auditAttrSignContent {
			return true
		}
		val, err := valueToInterface(kv.Value, 0)
		if err != nil {
			walkErr = err
			return false
		}
		if val != nil {
			attrs[key] = val
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if !mergeAudit {
		return attrs, nil
	}
	if err := mergeAuditFieldsIntoAttributes(attrs, record); err != nil {
		return nil, err
	}
	return attrs, nil
}

func mergeAuditFieldsIntoAttributes(attrs map[string]any, record AuditRecord) error {
	putValue := func(key string, v log.Value) error {
		if v.Kind() == log.KindEmpty {
			return nil
		}
		typed, err := valueToInterface(v, 0)
		if err != nil {
			return err
		}
		if typed != nil {
			attrs[key] = typed
		}
		return nil
	}
	putString := func(key, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		attrs[key] = map[string]any{"stringValue": v}
	}

	if err := putValue(auditAttrActor, record.Actor); err != nil {
		return err
	}
	putString(auditAttrActorType, record.ActorType)
	putString(auditAttrAction, record.Action)
	targetID, targetType := auditTargetFields(record)
	putString(auditAttrTargetID, targetID)
	putString(auditAttrTargetType, targetType)
	putString(auditAttrOutcome, record.Outcome)
	putString(auditAttrSourceID, record.SourceIP)
	putString(auditAttrSourceType, record.SourceType)
	putString(auditAttrSchemaVersion, record.SchemaVersion)
	putString(auditAttrRecordID, record.RecordID)
	if record.SequenceNo > 0 {
		attrs[auditAttrSequenceNo] = map[string]any{"intValue": strconv.FormatInt(record.SequenceNo, 10)}
	}
	putString(auditAttrPrevHash, record.PrevHash)
	return nil
}

func valueToInterface(v log.Value, depth int) (any, error) {
	if depth > jsonMaxDepth {
		return nil, fmt.Errorf("value exceeds nesting depth limit (%d)", jsonMaxDepth)
	}
	switch v.Kind() {
	case log.KindEmpty:
		return nil, nil
	case log.KindString:
		s := v.AsString()
		if !utf8.ValidString(s) {
			return nil, errors.New("string value contains invalid UTF-8")
		}
		return map[string]any{"stringValue": s}, nil
	case log.KindInt64:
		return map[string]any{"intValue": strconv.FormatInt(v.AsInt64(), 10)}, nil
	case log.KindFloat64:
		return map[string]any{"doubleValue": v.AsFloat64()}, nil
	case log.KindBool:
		return map[string]any{"boolValue": v.AsBool()}, nil
	case log.KindBytes:
		return map[string]any{"bytesValue": base64.StdEncoding.EncodeToString(v.AsBytes())}, nil
	case log.KindSlice:
		raw := v.AsSlice()
		slice := make([]any, len(raw))
		for i, elem := range raw {
			val, err := valueToInterface(elem, depth+1)
			if err != nil {
				return nil, err
			}
			slice[i] = val
		}
		return slice, nil
	case log.KindMap:
		raw := v.AsMap()
		m := make(map[string]any, len(raw))
		for _, kv := range raw {
			converted, err := valueToInterface(kv.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m[string(kv.Key)] = converted
		}
		return m, nil
	default:
		return nil, nil
	}
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
