// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

const (
	attrRecordID = "audit.record_id"
	attrHash     = "audit.hash"
	attrHMAC     = "audit.hmac"
)

func GetRecordID(record *sdklog.Record) (string, error) {
	if record == nil {
		return "", fmt.Errorf("record cannot be nil")
	}
	var id string
	record.WalkAttributes(func(kv log.KeyValue) bool {
		if string(kv.Key) != attrRecordID {
			return true
		}
		var v string
		if kv.Value.Kind() == log.KindString {
			v = strings.TrimSpace(kv.Value.AsString())
		} else {
			v = strings.TrimSpace(kv.Value.String())
		}
		if v != "" {
			id = v
		}
		return true
	})
	if id != "" {
		return id, nil
	}
	body := ""
	if record.Body().Kind() != log.KindEmpty {
		body = record.Body().String()
	}
	return fmt.Sprintf("%d_%s_%s", record.Timestamp().UnixNano(), body, record.Severity().String()), nil
}

func GetRecordHash(record *sdklog.Record) string {
	if record == nil {
		return ""
	}
	if h := recordIntegrityAttr(record, attrHMAC); h != "" {
		return h
	}
	return recordIntegrityAttr(record, attrHash)
}

func recordIntegrityAttr(record *sdklog.Record, key string) string {
	var value string
	record.WalkAttributes(func(kv log.KeyValue) bool {
		if string(kv.Key) != key {
			return true
		}
		if kv.Value.Kind() == log.KindString {
			value = strings.TrimSpace(kv.Value.AsString())
		} else {
			value = strings.TrimSpace(kv.Value.String())
		}
		return false
	})
	return value
}
