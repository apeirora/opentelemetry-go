// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "testing"

func TestAuditIntegrityFieldsHelpers(t *testing.T) {
	var none AuditIntegrityFields
	if none.AnySet() {
		t.Fatal("expected AnySet to be false for zero value")
	}
	if none.Has(AuditIntegrityHMAC) {
		t.Fatal("expected Has to be false for zero value")
	}
	all := AuditIntegrityHMAC | AuditIntegrityHash | AuditIntegritySignature
	if !all.AnySet() {
		t.Fatal("expected AnySet to be true when bits are set")
	}
	if !all.Has(AuditIntegrityHMAC) || !all.Has(AuditIntegrityHash) || !all.Has(AuditIntegritySignature) {
		t.Fatal("expected Has to be true for set bits")
	}
}

func TestNormalizeAuditSignContent(t *testing.T) {
	tests := []struct {
		in   string
		want AuditSignContent
	}{
		{in: "body", want: AuditSignContentBody},
		{in: " BODY ", want: AuditSignContentBody},
		{in: "attr", want: AuditSignContentAttr},
		{in: "attributes", want: AuditSignContentAttr},
		{in: "meta", want: AuditSignContentMeta},
		{in: "", want: AuditSignContentMeta},
		{in: "unknown", want: AuditSignContentMeta},
	}
	for _, tt := range tests {
		if got := normalizeAuditSignContent(tt.in); got != tt.want {
			t.Fatalf("normalizeAuditSignContent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDefaultIntegritySets(t *testing.T) {
	if got := defaultRequiredIntegrity(); !got.Has(AuditIntegrityHMAC) || !got.Has(AuditIntegritySignature) || got.Has(AuditIntegrityHash) {
		t.Fatalf("unexpected defaultRequiredIntegrity value: %v", got)
	}
	if got := defaultExportIntegrity(); !got.Has(AuditIntegrityHMAC) || !got.Has(AuditIntegritySignature) || !got.Has(AuditIntegrityHash) {
		t.Fatalf("unexpected defaultExportIntegrity value: %v", got)
	}
}

func TestDefaultLegacyAutoSignIntegrity(t *testing.T) {
	if got := defaultLegacyAutoSignIntegrity(true); got != AuditIntegrityHMAC {
		t.Fatalf("expected AuditIntegrityHMAC when key exists, got %v", got)
	}
	if got := defaultLegacyAutoSignIntegrity(false); got != 0 {
		t.Fatalf("expected zero integrity when key is absent, got %v", got)
	}
}
