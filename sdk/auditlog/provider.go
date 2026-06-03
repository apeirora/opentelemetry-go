// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"log/slog"
	"sync/atomic"

	"go.opentelemetry.io/otel/audit"
)

// SdkAuditProvider implements audit.AuditProvider using AuditLoggerProvider.
type SdkAuditProvider struct {
	inner   *AuditLoggerProvider
	shutdown atomic.Bool
}

// NewSdkAuditProvider returns an audit.AuditProvider backed by the auditlog SDK.
func NewSdkAuditProvider(opts ...AuditLoggerProviderOption) *SdkAuditProvider {
	return &SdkAuditProvider{inner: NewAuditLoggerProvider(opts...)}
}

// NewSdkAuditProviderWithProcessor wires a pre-built processor into the provider.
func NewSdkAuditProviderWithProcessor(processor *AuditLogProcessor) *SdkAuditProvider {
	return &SdkAuditProvider{inner: NewAuditLoggerProviderWithProcessor(processor)}
}

func (p *SdkAuditProvider) Logger(name string, opts ...audit.LoggerOption) audit.AuditLogger {
	if name == "" {
		slog.Warn("audit: logger created with empty name; using 'unknown'")
		name = "unknown"
	}
	cfg := audit.ApplyLoggerOptions(opts...)
	return &sdkAuditLogger{
		inner: p.inner.Logger(name, WithAuditLoggerVersion(cfg.Version), WithAuditLoggerSchemaURL(cfg.SchemaURL)),
	}
}

func (p *SdkAuditProvider) Shutdown(ctx context.Context) error {
	p.shutdown.Store(true)
	return p.inner.Shutdown(ctx)
}

func (p *SdkAuditProvider) ForceFlush(ctx context.Context) error {
	return p.inner.ForceFlush(ctx)
}

type sdkAuditLogger struct {
	inner AuditLogger
}

func (l *sdkAuditLogger) Emit(ctx context.Context, record audit.AuditRecord) (audit.AuditReceipt, error) {
	sdkRec, err := auditRecordToSDK(record)
	if err != nil {
		return audit.AuditReceipt{}, err
	}
	return l.inner.Emit(ctx, sdkRec)
}

func auditRecordToSDK(record audit.AuditRecord) (AuditRecord, error) {
	sdk := AuditRecord{
		EventName:            record.EventName,
		Actor:                record.Actor,
		ActorType:            record.ActorType,
		Action:               record.Action,
		TargetID:             record.TargetID,
		TargetType:           record.TargetType,
		Outcome:              record.Outcome,
		SourceIP:             record.SourceIP,
		SourceType:           record.SourceType,
		RecordID:             record.RecordID,
		SchemaVersion:        record.SchemaVersion,
		SequenceNo:           record.SequenceNo,
		PrevHash:             record.PrevHash,
		Hash:                 record.Hash,
		HMAC:                 record.HMAC,
		Signature:            record.Signature,
		IntegrityValue:       record.IntegrityValue,
		IntegrityAlgorithm:   record.IntegrityAlgorithm,
		IntegrityCertificate: record.IntegrityCertificate,
		SignContent:          record.SignContent,
		HashAlgorithm:        record.HashAlgorithm,
		KeyID:                record.KeyID,
	}
	if !record.Timestamp.IsZero() {
		sdk.SetTimestamp(record.Timestamp)
	}
	if !record.ObservedTimestamp.IsZero() {
		sdk.SetObservedTimestamp(record.ObservedTimestamp)
	}
	if record.Body.Kind() != 0 {
		sdk.SetBody(record.Body)
	}
	for _, kv := range record.Attributes {
		sdk.AddAttributes(kv)
	}
	return sdk, nil
}
