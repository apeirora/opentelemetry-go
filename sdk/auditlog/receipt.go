// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"strings"
	"time"

	"go.opentelemetry.io/otel/audit"
	"go.opentelemetry.io/otel/log"
)

const auditAttrRecordIDReceipt = "audit.record.id"

func receiptsFromRecords(records []Record) []audit.AuditReceipt {
	out := make([]audit.AuditReceipt, 0, len(records))
	now := time.Now().UTC()
	for i := range records {
		rec := records[i]
		id := recordIDFromSDKRecord(rec)
		hash, _ := integrityHashFromSDKRecord(rec)
		out = append(out, audit.AuditReceipt{
			RecordID:      id,
			IntegrityHash: hash,
			SinkTimestamp: now,
		})
	}
	return out
}

func recordIDFromSDKRecord(rec Record) string {
	var id string
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		if string(kv.Key) != auditAttrRecordID {
			return true
		}
		if kv.Value.Kind() == log.KindString {
			id = strings.TrimSpace(kv.Value.AsString())
		} else {
			id = strings.TrimSpace(kv.Value.String())
		}
		return false
	})
	return id
}

func integrityHashFromSDKRecord(rec Record) (string, error) {
	ar, err := auditRecordFromSDKRecord(rec)
	if err != nil {
		return "", err
	}
	payload, err := jcsSigningPayload(ar)
	if err != nil {
		return "", err
	}
	return sha256Hex(payload)
}

func auditRecordFromSDKRecord(rec Record) (AuditRecord, error) {
	ar := AuditRecord{Record: rec}
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		key := string(kv.Key)
		val := attrStringValue(kv)
		switch key {
		case auditAttrRecordID:
			ar.RecordID = val
		case auditAttrActor:
			ar.Actor = kv.Value
		case auditAttrActorType:
			ar.ActorType = val
		case auditAttrAction:
			ar.Action = val
		case auditAttrOutcome:
			ar.Outcome = val
		case auditAttrTargetID:
			ar.TargetID = val
		case auditAttrTargetType:
			ar.TargetType = val
		case auditAttrSourceID:
			ar.SourceIP = val
		case auditAttrSourceType:
			ar.SourceType = val
		case auditAttrSchemaVersion:
			ar.SchemaVersion = val
		case auditAttrHash:
			ar.Hash = val
		case auditAttrHMAC:
			ar.HMAC = val
		case auditAttrSignature:
			ar.Signature = val
		case auditAttrIntegrityValue:
			ar.IntegrityValue = val
		case auditAttrIntegrityAlgorithm:
			ar.IntegrityAlgorithm = val
		case auditAttrIntegrityCertificate:
			ar.IntegrityCertificate = val
		case auditAttrPrevHash, "audit.prev_hash":
			ar.PrevHash = val
		case auditAttrSequenceNo:
			if kv.Value.Kind() == log.KindInt64 {
				ar.SequenceNo = kv.Value.AsInt64()
			}
		}
		return true
	})
	ar.EventName = rec.EventName()
	return ar, nil
}

func integrityHashForAuditRecord(record AuditRecord) (string, error) {
	payload, err := jcsSigningPayload(record)
	if err != nil {
		return "", err
	}
	return sha256Hex(payload)
}

func attrStringValue(kv log.KeyValue) string {
	if kv.Value.Kind() == log.KindString {
		return strings.TrimSpace(kv.Value.AsString())
	}
	return strings.TrimSpace(kv.Value.String())
}
