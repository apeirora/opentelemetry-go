// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"log/slog"
	"sync"
	"time"
)

const (
	defaultAuditTimestampSkew   = 5 * time.Second
	auditClockStartupWarnMessage = "audit: verify host clock is NTP-synchronized; audit compliance requires accurate timestamps (recommended offset < 1s)"
)

var auditClockStartupOnce sync.Once

func warnAuditClockSyncOnce() {
	auditClockStartupOnce.Do(func() {
		slog.Warn(auditClockStartupWarnMessage)
	})
}

func warnAuditRecordTimestampSkew(record AuditRecord, maxSkew time.Duration) {
	if maxSkew <= 0 {
		return
	}
	ts := record.Timestamp()
	obs := record.ObservedTimestamp()
	if ts.IsZero() || obs.IsZero() {
		return
	}
	skew := ts.Sub(obs)
	if skew < 0 {
		skew = -skew
	}
	if skew <= maxSkew {
		return
	}
	slog.Warn(
		"audit: record timestamp skew exceeds threshold",
		"skew", skew.String(),
		"threshold", maxSkew.String(),
		"record_id", record.RecordID,
	)
}
