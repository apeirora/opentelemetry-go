// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/log"
)

func getAuditRecordID(record *Record) (string, error) {
	if record == nil {
		return "", fmt.Errorf("record cannot be nil")
	}
	var id string
	record.WalkAttributes(func(kv log.KeyValue) bool {
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
	if id != "" {
		return id, nil
	}
	body := ""
	if record.Body().Kind() != log.KindEmpty {
		body = record.Body().String()
	}
	return fmt.Sprintf("%d_%s_%s", record.Timestamp().UnixNano(), body, record.Severity().String()), nil
}

func getAuditRecordHash(record *Record) string {
	if record == nil {
		return ""
	}
	var hash string
	record.WalkAttributes(func(kv log.KeyValue) bool {
		if string(kv.Key) != auditAttrHash {
			return true
		}
		if kv.Value.Kind() == log.KindString {
			hash = strings.TrimSpace(kv.Value.AsString())
		} else {
			hash = strings.TrimSpace(kv.Value.String())
		}
		return false
	})
	return hash
}
