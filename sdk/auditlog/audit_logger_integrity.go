// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"strings"
)

func (p *AuditLoggerProvider) applyDefaultSignContent(record AuditRecord) AuditRecord {
	if strings.TrimSpace(record.SignContent) != "" {
		return record
	}
	if p.signContent == "" {
		return record
	}
	record.SignContent = string(p.signContent)
	return record
}

func (p *AuditLoggerProvider) enrichIntegrity(ctx context.Context, record AuditRecord) (AuditRecord, error) {
	if p.integrityEnricher != nil {
		return p.integrityEnricher(ctx, record)
	}
	record = p.applyDefaultSignContent(record)
	auto := p.autoSignIntegrity
	if auto.Has(AuditIntegrityHash) && !hasHashIntegrity(record) {
		var err error
		if p.hashComputer != nil {
			record, err = p.hashComputer(record, p.hashAlgorithm)
		} else {
			record, err = signAuditRecordHash(record, p.hashAlgorithm)
		}
		if err != nil {
			return record, err
		}
	}
	if auto.Has(AuditIntegrityHMAC) && !hasMACIntegrity(record) && len(p.hmacVerificationKey) > 0 {
		var err error
		if p.hmacSigner != nil {
			record, err = p.hmacSigner(record, p.hmacVerificationKey, p.hashAlgorithm)
		} else {
			record, err = signAuditRecordHMAC(record, p.hmacVerificationKey, p.hashAlgorithm)
		}
		if err != nil {
			return record, err
		}
	}
	if auto.Has(AuditIntegritySignature) && !hasSignatureIntegrity(record) && p.signatureSigner != nil {
		var err error
		record, err = signAuditRecordSignature(record, p.signatureSigner)
		if err != nil {
			return record, err
		}
	}
	return record, nil
}

func hasHashIntegrity(record AuditRecord) bool {
	return record.IntegrityValue != "" && isHashIntegrityAlgorithm(record.IntegrityAlgorithm)
}

func hasMACIntegrity(record AuditRecord) bool {
	return record.IntegrityValue != "" && isMACIntegrityAlgorithm(record.IntegrityAlgorithm)
}

func hasSignatureIntegrity(record AuditRecord) bool {
	if record.IntegrityValue == "" {
		return false
	}
	alg := strings.TrimSpace(record.IntegrityAlgorithm)
	return alg != "" && !isMACIntegrityAlgorithm(alg) && !isHashIntegrityAlgorithm(alg)
}

func (p *AuditLoggerProvider) satisfiesRequiredIntegrity(record AuditRecord) bool {
	req := p.requiredIntegrity
	if !req.AnySet() {
		return true
	}
	if req.Has(AuditIntegrityHMAC) && hasMACIntegrity(record) {
		return true
	}
	if req.Has(AuditIntegrityHash) && hasHashIntegrity(record) {
		return true
	}
	if req.Has(AuditIntegritySignature) && hasSignatureIntegrity(record) {
		return true
	}
	return false
}
