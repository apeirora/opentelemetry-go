// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
)

var (
	validAuditActorTypes = map[string]struct{}{
		"user": {}, "service": {}, "system": {},
	}
	validAuditOutcomes = map[string]struct{}{
		"success": {}, "failure": {}, "unknown": {},
	}
)

func normalizeAuditRecordFields(record *AuditRecord) {
	record.Action = strings.ToUpper(strings.TrimSpace(record.Action))
	record.ActorType = strings.ToLower(strings.TrimSpace(record.ActorType))
	record.Outcome = strings.ToLower(strings.TrimSpace(record.Outcome))
}

func ensureAuditRecordID(record *AuditRecord) error {
	id := strings.TrimSpace(record.RecordID)
	if id == "" {
		record.RecordID = uuid.NewString()
		return nil
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 4 {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit.record.id must be a UUID v4", false, err)
	}
	record.RecordID = parsed.String()
	return nil
}

func validateAuditRecordSpec(record AuditRecord) error {
	if record.Actor.Kind() == log.KindEmpty || strings.TrimSpace(record.Actor.String()) == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit.actor.id is required", false, nil)
	}
	if _, ok := validAuditActorTypes[record.ActorType]; !ok {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit.actor.type must be user, service, or system", false, nil)
	}
	if record.Action == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit action is required", false, nil)
	}
	if _, ok := validAuditOutcomes[record.Outcome]; !ok {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit.outcome must be success, failure, or unknown", false, nil)
	}
	return nil
}

func isMACIntegrityAlgorithm(alg string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(alg)), "HMAC-")
}

func isHashIntegrityAlgorithm(alg string) bool {
	switch strings.ToUpper(strings.TrimSpace(alg)) {
	case "SHA256", "SHA512":
		return true
	default:
		return false
	}
}

func hashIntegrityAlgorithm(alg string) string {
	switch normalizeHashAlgorithm(alg) {
	case "sha512":
		return "SHA512"
	default:
		return "SHA256"
	}
}

func (p *AuditLoggerProvider) resolveResourceIntegrityAlgorithm(record AuditRecord) string {
	if alg := strings.TrimSpace(record.IntegrityAlgorithm); alg != "" {
		return alg
	}
	return strings.TrimSpace(p.resourceIntegrityAlgorithm)
}

func (p *AuditLoggerProvider) resolveResourceIntegrityCertificate(record AuditRecord, algorithm string) string {
	if isMACIntegrityAlgorithm(algorithm) {
		return ""
	}
	if cert := strings.TrimSpace(record.IntegrityCertificate); cert != "" {
		return cert
	}
	if cert := strings.TrimSpace(record.KeyID); cert != "" {
		return cert
	}
	return strings.TrimSpace(p.resourceIntegrityCertificate)
}

func (p *AuditLoggerProvider) auditResourceForRecord(record AuditRecord) (*sdkresource.Resource, error) {
	base := p.resource
	if base == nil {
		base = sdkresource.Empty()
	}
	algorithm := p.resolveResourceIntegrityAlgorithm(record)
	certificate := p.resolveResourceIntegrityCertificate(record, algorithm)
	hasValue := record.IntegrityValue != "" && p.exportIntegrity.AnySet()
	if hasValue && algorithm == "" {
		return nil, newAuditStatusError(AuditErrorInvalidRequest, "audit.integrity.algorithm is required when audit.integrity.value is present", false, nil)
	}
	if algorithm == "" && certificate == "" {
		return base, nil
	}
	attrs := make([]attribute.KeyValue, 0, 2)
	if algorithm != "" {
		attrs = append(attrs, attribute.String(auditAttrIntegrityAlgorithm, algorithm))
	}
	if certificate != "" {
		attrs = append(attrs, attribute.String(auditAttrIntegrityCertificate, certificate))
	}
	extra := sdkresource.NewSchemaless(attrs...)
	merged, err := sdkresource.Merge(base, extra)
	if err != nil {
		return nil, err
	}
	return merged, nil
}

func prepareAuditLogRecord(otelRecord *Record) {
	otelRecord.SetSeverity(0)
	otelRecord.SetSeverityText("")
}
