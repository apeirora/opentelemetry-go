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
	clearHashOnHMAC := auto.Has(AuditIntegrityHMAC) && !auto.Has(AuditIntegrityHash)
	if auto.Has(AuditIntegrityHash) && record.Hash == "" {
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
	if auto.Has(AuditIntegrityHMAC) && record.HMAC == "" && len(p.hmacVerificationKey) > 0 {
		var err error
		if p.hmacSigner != nil {
			record, err = p.hmacSigner(record, p.hmacVerificationKey, p.hashAlgorithm)
		} else {
			record, err = signAuditRecordHMAC(record, p.hmacVerificationKey, p.hashAlgorithm, clearHashOnHMAC)
		}
		if err != nil {
			return record, err
		}
	}
	if auto.Has(AuditIntegritySignature) && record.Signature == "" && p.signatureSigner != nil {
		var err error
		record, err = signAuditRecordSignature(record, p.signatureSigner)
		if err != nil {
			return record, err
		}
	}
	return record, nil
}

func (p *AuditLoggerProvider) satisfiesRequiredIntegrity(record AuditRecord) bool {
	req := p.requiredIntegrity
	if !req.AnySet() {
		return record.Signature != "" || record.HMAC != "" || record.IntegrityValue != ""
	}
	if req.Has(AuditIntegrityHMAC) && (record.HMAC != "" || record.IntegrityValue != "") {
		return true
	}
	if req.Has(AuditIntegrityHash) && record.Hash != "" {
		return true
	}
	if req.Has(AuditIntegritySignature) && record.Signature != "" {
		return true
	}
	return false
}
