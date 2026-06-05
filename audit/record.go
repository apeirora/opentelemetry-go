// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"time"

	"go.opentelemetry.io/otel/log"
)

// AuditRecord is a log record carrying audit-specific data.
type AuditRecord struct {
	Timestamp         time.Time
	ObservedTimestamp time.Time
	EventName         string
	Body              log.Value
	Attributes        []log.KeyValue

	Actor         log.Value
	ActorType     string
	Action        string
	TargetID      string
	TargetType    string
	Outcome       string
	SourceIP      string
	SourceType    string
	RecordID      string
	SchemaVersion string
	SequenceNo    int64
	PrevHash      string

	IntegrityValue         string
	IntegrityAlgorithm     string
	IntegrityCertificate   string
	SignContent            string
	HashAlgorithm          string
	KeyID                  string
}
