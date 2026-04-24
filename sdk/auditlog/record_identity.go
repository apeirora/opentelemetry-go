// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"go.opentelemetry.io/otel/sdk/auditlog/identity"
)

func getAuditRecordID(record *Record) (string, error) {
	return identity.GetRecordID(record)
}

func getAuditRecordHash(record *Record) string {
	return identity.GetRecordHash(record)
}
