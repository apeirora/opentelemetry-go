// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "strings"

type AuditIntegrityFields uint8

const (
	AuditIntegrityHMAC AuditIntegrityFields = 1 << iota
	AuditIntegrityHash
	AuditIntegritySignature
)

func (f AuditIntegrityFields) Has(x AuditIntegrityFields) bool {
	return f&x != 0
}

func (f AuditIntegrityFields) AnySet() bool {
	return f != 0
}

type AuditSignContent string

const (
	AuditSignContentMeta AuditSignContent = "meta"
	AuditSignContentBody AuditSignContent = "body"
	AuditSignContentAttr AuditSignContent = "attr"
)

func normalizeAuditSignContent(mode string) AuditSignContent {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "body":
		return AuditSignContentBody
	case "attr", "attributes":
		return AuditSignContentAttr
	default:
		return AuditSignContentMeta
	}
}

func defaultRequiredIntegrity() AuditIntegrityFields {
	return AuditIntegrityHMAC | AuditIntegritySignature
}

func defaultExportIntegrity() AuditIntegrityFields {
	return AuditIntegrityHMAC | AuditIntegritySignature | AuditIntegrityHash
}

func defaultLegacyAutoSignIntegrity(hasHMACKey bool) AuditIntegrityFields {
	if hasHMACKey {
		return AuditIntegrityHMAC
	}
	return 0
}
