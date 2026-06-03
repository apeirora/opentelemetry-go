// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package audit

import "time"

// AuditReceipt is proof of delivery returned after the audit sink acknowledges a record.
type AuditReceipt struct {
	RecordID      string
	IntegrityHash string
	SinkTimestamp time.Time
}
