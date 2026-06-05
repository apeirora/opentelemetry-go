// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/audit"
	"go.opentelemetry.io/otel/log"
	"golang.org/x/time/rate"
)

type AuditRecordProcessor interface {
	OnEmit(ctx context.Context, record *Record) error
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
}

type AuditLogger interface {
	Emit(ctx context.Context, record AuditRecord) (audit.AuditReceipt, error)
	EmitWithResult(ctx context.Context, record AuditRecord) AuditEmitResult
}

type AuditEmitResult struct {
	RecordID      string
	StatusCode    int
	Status        string
	Hash          string
	SinkTimestamp time.Time
	Reason        string
	RetryAfter    time.Duration
	QueuedAt      time.Time
}

type auditLogger struct {
	provider *AuditLoggerProvider
}

const (
	auditAttrActor         = "audit.actor.id"
	auditAttrActorType     = "audit.actor.type"
	auditAttrAction        = "audit.action"
	auditAttrTargetID      = "audit.target.id"
	auditAttrTargetType    = "audit.target.type"
	auditAttrOutcome       = "audit.outcome"
	auditAttrSourceID      = "audit.source.id"
	auditAttrRecordID      = "audit.record.id"
	auditAttrIntegrityValue     = "audit.integrity.value"
	auditAttrSchemaVersion = "audit.schema.version"
	auditAttrKeyID         = "audit.key_id"
	auditAttrSequenceNo    = "audit.sequence.number"
	auditAttrPrevHash      = "audit.prev.hash"
)

func auditTargetFields(record AuditRecord) (id, typ string) {
	id = strings.TrimSpace(record.TargetID)
	typ = strings.TrimSpace(record.TargetType)
	if id == "" && record.Resource.Kind() != log.KindEmpty {
		id = strings.TrimSpace(record.Resource.String())
	}
	return id, typ
}

func auditRecordIDAttrMatches(r Record, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	var match bool
	r.WalkAttributes(func(kv log.KeyValue) bool {
		if string(kv.Key) != auditAttrRecordID {
			return true
		}
		var v string
		if kv.Value.Kind() == log.KindString {
			v = strings.TrimSpace(kv.Value.AsString())
		} else {
			v = strings.TrimSpace(kv.Value.String())
		}
		if v == want {
			match = true
			return false
		}
		return true
	})
	return match
}

func (l *auditLogger) Emit(ctx context.Context, record AuditRecord) (audit.AuditReceipt, error) {
	result := l.EmitWithResult(ctx, record)
	if result.StatusCode >= 400 {
		return audit.AuditReceipt{}, newAuditStatusError(AuditErrorUnavailable, result.Reason, true, nil)
	}
	return audit.AuditReceipt{
		RecordID:      result.RecordID,
		IntegrityHash: result.Hash,
		SinkTimestamp: result.SinkTimestamp,
	}, nil
}

func (l *auditLogger) EmitWithResult(ctx context.Context, record AuditRecord) AuditEmitResult {
	if record.RecordID == "" {
		record.RecordID = uuid.NewString()
	}
	result := AuditEmitResult{
		RecordID: record.RecordID,
		Hash:     record.Hash,
	}
	if l.provider.stopped.Load() {
		err := newAuditStatusError(AuditErrorUnavailable, "provider_shutdown", true, nil)
		result.StatusCode, result.Status, result.Reason = mapAuditError(err)
		result.RetryAfter = time.Second
		return result
	}
	if err := l.provider.evaluatePolicies(ctx, record); err != nil {
		result.StatusCode, result.Status, result.Reason = mapAuditError(err)
		if statusErr, ok := err.(*AuditStatusError); ok && statusErr.Code == AuditErrorTooManyRequests {
			result.RetryAfter = time.Second
		}
		return result
	}
	record, err := l.provider.enrichIntegrity(ctx, record)
	if err != nil {
		result.StatusCode, result.Status, result.Reason = mapAuditError(
			newAuditStatusError(AuditErrorInvalidRequest, "audit integrity enrichment failed", false, err),
		)
		return result
	}
	result.Hash = record.Hash
	if err := validateRequiredAuditRecord(record, l.provider); err != nil {
		result.StatusCode, result.Status, result.Reason = mapAuditError(err)
		return result
	}
	if err := verifyAuditIntegrity(
		record,
		l.provider.hmacVerificationKey,
		l.provider.signatureVerifier,
		l.provider.hashAlgorithm,
		l.provider.signContent,
	); err != nil {
		result.StatusCode, result.Status, result.Reason = mapAuditError(err)
		return result
	}
	otelRecord := record.Record.Clone()
	if otelRecord.ObservedTimestamp().IsZero() {
		otelRecord.SetObservedTimestamp(time.Now())
	}
	record.SetObservedTimestamp(otelRecord.ObservedTimestamp())
	warnAuditRecordTimestampSkew(record, defaultAuditTimestampSkew)
	otelRecord.SetEventName(record.EventName)
	targetID, targetType := auditTargetFields(record)
	auditAttrs := []log.KeyValue{
		log.KeyValue{Key: auditAttrActor, Value: record.Actor},
		log.String(auditAttrActorType, record.ActorType),
		log.String(auditAttrAction, record.Action),
		log.String(auditAttrOutcome, record.Outcome),
	}
	if targetID != "" {
		auditAttrs = append(auditAttrs, log.String(auditAttrTargetID, targetID))
	}
	if targetType != "" {
		auditAttrs = append(auditAttrs, log.String(auditAttrTargetType, targetType))
	}
	if record.SchemaVersion != "" {
		auditAttrs = append(auditAttrs, log.String(auditAttrSchemaVersion, record.SchemaVersion))
	}
	if !auditRecordIDAttrMatches(otelRecord, record.RecordID) {
		auditAttrs = append(auditAttrs, log.String(auditAttrRecordID, record.RecordID))
	}
	otelRecord.AddAttributes(auditAttrs...)
	if record.SourceIP != "" {
		otelRecord.AddAttributes(log.String(auditAttrSourceID, record.SourceIP))
	}
	if l.provider.exportIntegrity.AnySet() {
		if record.IntegrityValue != "" {
			otelRecord.AddAttributes(log.String(auditAttrIntegrityValue, record.IntegrityValue))
		}
		if record.IntegrityAlgorithm != "" {
			otelRecord.AddAttributes(log.String(auditAttrIntegrityAlgorithm, record.IntegrityAlgorithm))
		}
		cert := record.IntegrityCertificate
		if cert == "" {
			cert = record.KeyID
		}
		if cert != "" {
			otelRecord.AddAttributes(log.String(auditAttrIntegrityCertificate, cert))
		}
	}
	if record.SequenceNo > 0 {
		otelRecord.AddAttributes(log.Int64(auditAttrSequenceNo, record.SequenceNo))
	}
	if record.PrevHash != "" {
		otelRecord.AddAttributes(log.String(auditAttrPrevHash, record.PrevHash))
	}
	if record.SourceType != "" {
		otelRecord.AddAttributes(log.String(auditAttrSourceType, record.SourceType))
	}
	if mode := strings.TrimSpace(record.SignContent); mode != "" {
		otelRecord.AddAttributes(log.String(auditAttrSignContent, mode))
	}
	queuedAt := time.Now().UTC()
	for _, p := range l.provider.processors {
		if err := p.OnEmit(ctx, &otelRecord); err != nil {
			mappedErr := newAuditStatusError(AuditErrorUnavailable, "processor_emit_failed", true, err)
			result.StatusCode, result.Status, result.Reason = mapAuditError(mappedErr)
			result.RetryAfter = time.Second
			return result
		}
	}
	auditMetricsInstance().recordEmitted(ctx, 1)
	if l.provider.shouldWaitOnExport() {
		for _, p := range l.provider.processors {
			if err := p.ForceFlush(ctx); err != nil {
				mappedErr := newAuditStatusError(AuditErrorUnavailable, "processor_flush_failed", true, err)
				result.StatusCode, result.Status, result.Reason = mapAuditError(mappedErr)
				result.RetryAfter = time.Second
				return result
			}
		}
		for _, proc := range l.provider.processors {
			if ap, ok := proc.(*AuditLogProcessor); ok {
				if dropErr := ap.takeDroppedError(record.RecordID); dropErr != nil {
					mappedErr := newAuditStatusError(AuditErrorUnavailable, "audit_record_dropped", true, dropErr)
					result.StatusCode, result.Status, result.Reason = mapAuditError(mappedErr)
					result.RetryAfter = time.Second
					return result
				}
			}
		}
		for _, proc := range l.provider.processors {
			if ap, ok := proc.(*AuditLogProcessor); ok {
				if rcpt, found := ap.ReceiptFor(record.RecordID); found {
					result.Hash = rcpt.IntegrityHash
					result.SinkTimestamp = rcpt.SinkTimestamp
					break
				}
			}
		}
		if result.SinkTimestamp.IsZero() {
			result.SinkTimestamp = time.Now().UTC()
		}
		if result.Hash == "" {
			if hash, err := integrityHashForAuditRecord(record); err == nil {
				result.Hash = hash
			}
		}
		result.StatusCode = 200
		result.Status = "delivered"
	} else {
		result.StatusCode = 202
		result.Status = "queued"
		result.QueuedAt = queuedAt
	}
	return result
}

func validateRequiredAuditRecord(record AuditRecord, p *AuditLoggerProvider) error {
	if record.Timestamp().IsZero() {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit timestamp is required", false, nil)
	}
	if record.EventName == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit event_name is required", false, nil)
	}
	if record.Actor.Kind() == log.KindEmpty {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit actor is required", false, nil)
	}
	if record.ActorType == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit actor_type is required", false, nil)
	}
	if record.Action == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit action is required", false, nil)
	}
	if record.Outcome == "" {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit outcome is required", false, nil)
	}
	if !p.satisfiesRequiredIntegrity(record) {
		return newAuditStatusError(AuditErrorInvalidRequest, "audit integrity proof is required", false, nil)
	}
	return nil
}

type AuditLoggerProvider struct {
	processors            []AuditRecordProcessor
	hmacVerificationKey   []byte
	signatureVerifier     AuditSignatureVerifier
	signatureSigner       AuditSignatureSigner
	hashAlgorithm         string
	signContent           AuditSignContent
	autoSignIntegrity     AuditIntegrityFields
	requiredIntegrity     AuditIntegrityFields
	exportIntegrity       AuditIntegrityFields
	explicitRecordSigning bool
	hmacSigner            AuditHMACSigner
	hashComputer          AuditHashComputer
	integrityEnricher     AuditIntegrityEnricher
	authorizer            AuditAuthorizer
	maxBodyBytes          int
	maxAttributeCount     int
	maxRequestsPerSecond  int
	rateLimiter           *rate.Limiter
	loggersMu             sync.Mutex
	loggers              map[auditLoggerKey]*auditLogger
	stopped               atomic.Bool
}

type auditLoggerKey struct {
	name      string
	version   string
	schemaURL string
}

type AuditHMACSigner func(record AuditRecord, key []byte, algorithm string) (AuditRecord, error)

type AuditHashComputer func(record AuditRecord, algorithm string) (AuditRecord, error)

type AuditIntegrityEnricher func(ctx context.Context, record AuditRecord) (AuditRecord, error)

type auditProviderConfig struct {
	processors            []AuditRecordProcessor
	hmacVerificationKey   []byte
	signatureVerifier     AuditSignatureVerifier
	signatureSigner       AuditSignatureSigner
	hashAlgorithm         string
	signContent           AuditSignContent
	autoSignIntegrity     AuditIntegrityFields
	requiredIntegrity     AuditIntegrityFields
	exportIntegrity       AuditIntegrityFields
	explicitRecordSigning bool
	hmacSigner            AuditHMACSigner
	hashComputer          AuditHashComputer
	integrityEnricher     AuditIntegrityEnricher
	authorizer            AuditAuthorizer
	maxBodyBytes          int
	maxAttributeCount     int
	maxRequestsPerSecond  int
}

type AuditLoggerProviderOption interface {
	apply(auditProviderConfig) auditProviderConfig
}

type auditLoggerProviderOptionFunc func(auditProviderConfig) auditProviderConfig

func (f auditLoggerProviderOptionFunc) apply(c auditProviderConfig) auditProviderConfig {
	return f(c)
}

func WithAuditRecordProcessor(processor AuditRecordProcessor) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.processors = append(cfg.processors, processor)
		return cfg
	})
}

// WithAuditRecordSigning configures integrity applied to every emitted record.
// fields selects which proofs the provider auto-computes (HMAC, hash, certificate signature).
// content selects the signed payload: AuditSignContentMeta (canonical), AuditSignContentBody, or AuditSignContentAttr.
// Required proofs match fields; use WithAuditRequiredIntegrity to override.
func WithAuditRecordSigning(fields AuditIntegrityFields, content AuditSignContent) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.explicitRecordSigning = true
		cfg.autoSignIntegrity = fields
		cfg.signContent = content
		cfg.requiredIntegrity = fields
		return cfg
	})
}

// WithAuditSignContent sets the default sign_content mode for every record (body, meta, attr).
func WithAuditSignContent(content AuditSignContent) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.signContent = content
		return cfg
	})
}

// WithAuditAutoSignIntegrity sets which integrity fields the provider computes when missing.
func WithAuditAutoSignIntegrity(fields AuditIntegrityFields) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.explicitRecordSigning = true
		cfg.autoSignIntegrity = fields
		return cfg
	})
}

// WithAuditRequiredIntegrity sets which integrity fields must be present (any one satisfies).
func WithAuditRequiredIntegrity(fields AuditIntegrityFields) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.requiredIntegrity = fields
		return cfg
	})
}

// WithAuditExportIntegrity sets which integrity fields are exported as log attributes.
func WithAuditExportIntegrity(fields AuditIntegrityFields) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.exportIntegrity = fields
		return cfg
	})
}

func WithAuditHMACSigner(signer AuditHMACSigner) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.hmacSigner = signer
		return cfg
	})
}

func WithAuditHashComputer(computer AuditHashComputer) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.hashComputer = computer
		return cfg
	})
}

func WithAuditSignatureSigner(signer AuditSignatureSigner) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.signatureSigner = signer
		return cfg
	})
}

func WithAuditIntegrityEnricher(enricher AuditIntegrityEnricher) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.integrityEnricher = enricher
		return cfg
	})
}

// WithAuditHMACVerificationKey configures the shared secret used to verify HMAC tags on
// incoming audit records. When the key is non-empty and record signing is not configured
// explicitly, the provider auto-computes HMAC before validation (trusted in-process emitters).
func WithAuditHMACVerificationKey(key []byte) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		if len(key) == 0 {
			cfg.hmacVerificationKey = nil
			return cfg
		}
		k := make([]byte, len(key))
		copy(k, key)
		cfg.hmacVerificationKey = k
		return cfg
	})
}

// AuditSignatureVerifier verifies a record signature. The payload bytes match those used
// when signing (see signingPayload and AuditSignContent).
type AuditSignatureVerifier func(record AuditRecord, payload []byte) error
type AuditAuthorizer func(ctx context.Context, record AuditRecord) error

func WithAuditSignatureVerifier(verifier AuditSignatureVerifier) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.signatureVerifier = verifier
		return cfg
	})
}

func WithAuditHashAlgorithm(algorithm string) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.hashAlgorithm = algorithm
		return cfg
	})
}

func WithAuditAuthorizer(authorizer AuditAuthorizer) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.authorizer = authorizer
		return cfg
	})
}

func WithAuditMaxBodyBytes(limit int) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.maxBodyBytes = limit
		return cfg
	})
}

func WithAuditMaxAttributeCount(limit int) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.maxAttributeCount = limit
		return cfg
	})
}

func WithAuditMaxRequestsPerSecond(limit int) AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		cfg.maxRequestsPerSecond = limit
		return cfg
	})
}

func resolveAuditProviderIntegrity(cfg auditProviderConfig) (autoSign, required, export AuditIntegrityFields, signContent AuditSignContent) {
	autoSign = cfg.autoSignIntegrity
	required = cfg.requiredIntegrity
	export = cfg.exportIntegrity
	signContent = cfg.signContent
	if !cfg.explicitRecordSigning {
		autoSign = defaultLegacyAutoSignIntegrity(len(cfg.hmacVerificationKey) > 0)
	}
	if !required.AnySet() && cfg.explicitRecordSigning && autoSign.AnySet() {
		required = autoSign
	}
	if !export.AnySet() {
		export = defaultExportIntegrity()
	}
	return autoSign, required, export, signContent
}

func NewAuditLoggerProvider(opts ...AuditLoggerProviderOption) *AuditLoggerProvider {
	cfg := auditProviderConfig{}
	for _, opt := range opts {
		cfg = opt.apply(cfg)
	}
	autoSign, required, export, signContent := resolveAuditProviderIntegrity(cfg)
	p := &AuditLoggerProvider{
		processors:            cfg.processors,
		hmacVerificationKey:   cfg.hmacVerificationKey,
		signatureVerifier:     cfg.signatureVerifier,
		signatureSigner:       cfg.signatureSigner,
		hashAlgorithm:         cfg.hashAlgorithm,
		signContent:           signContent,
		autoSignIntegrity:     autoSign,
		requiredIntegrity:     required,
		exportIntegrity:       export,
		explicitRecordSigning: cfg.explicitRecordSigning,
		hmacSigner:            cfg.hmacSigner,
		hashComputer:          cfg.hashComputer,
		integrityEnricher:     cfg.integrityEnricher,
		authorizer:            cfg.authorizer,
		maxBodyBytes:          cfg.maxBodyBytes,
		maxAttributeCount:     cfg.maxAttributeCount,
		maxRequestsPerSecond:  cfg.maxRequestsPerSecond,
		loggers:               make(map[auditLoggerKey]*auditLogger),
	}
	if cfg.maxRequestsPerSecond > 0 {
		lim := rate.Limit(cfg.maxRequestsPerSecond)
		p.rateLimiter = rate.NewLimiter(lim, cfg.maxRequestsPerSecond)
	}
	warnAuditClockSyncOnce()
	_ = auditMetricsInstance()
	return p
}

type AuditLoggerOption interface {
	apply(auditLoggerConfig) auditLoggerConfig
}

type auditLoggerConfig struct {
	version   string
	schemaURL string
}

type auditLoggerOptionFunc func(auditLoggerConfig) auditLoggerConfig

func (f auditLoggerOptionFunc) apply(c auditLoggerConfig) auditLoggerConfig {
	return f(c)
}

func WithAuditLoggerVersion(version string) AuditLoggerOption {
	return auditLoggerOptionFunc(func(c auditLoggerConfig) auditLoggerConfig {
		c.version = version
		return c
	})
}

func WithAuditLoggerSchemaURL(schemaURL string) AuditLoggerOption {
	return auditLoggerOptionFunc(func(c auditLoggerConfig) auditLoggerConfig {
		c.schemaURL = schemaURL
		return c
	})
}

func (p *AuditLoggerProvider) Logger(name string, opts ...AuditLoggerOption) AuditLogger {
	if p.stopped.Load() {
		return &auditLogger{provider: p}
	}
	cfg := auditLoggerConfig{}
	for _, opt := range opts {
		cfg = opt.apply(cfg)
	}
	key := auditLoggerKey{name: name, version: cfg.version, schemaURL: cfg.schemaURL}
	p.loggersMu.Lock()
	defer p.loggersMu.Unlock()
	if l, ok := p.loggers[key]; ok {
		return l
	}
	l := &auditLogger{provider: p}
	p.loggers[key] = l
	return l
}

func (p *AuditLoggerProvider) Shutdown(ctx context.Context) error {
	if p.stopped.Swap(true) {
		return nil
	}
	var firstErr error
	for _, proc := range p.processors {
		if err := proc.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *AuditLoggerProvider) ForceFlush(ctx context.Context) error {
	if p.stopped.Load() {
		return nil
	}
	var firstErr error
	for _, proc := range p.processors {
		if err := proc.ForceFlush(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *AuditLoggerProvider) shouldWaitOnExport() bool {
	for _, processor := range p.processors {
		if ap, ok := processor.(*AuditLogProcessor); ok && ap.config.WaitOnExport {
			return true
		}
	}
	return false
}

func (p *AuditLoggerProvider) evaluatePolicies(ctx context.Context, record AuditRecord) error {
	if p.authorizer != nil {
		if err := p.authorizer(ctx, record); err != nil {
			if _, ok := err.(*AuditStatusError); ok {
				return err
			}
			return newAuditStatusError(AuditErrorForbidden, "authorization_failed", false, err)
		}
	}
	if p.maxBodyBytes > 0 && len(record.Body().String()) > p.maxBodyBytes {
		return newAuditStatusError(AuditErrorPayloadTooLarge, "audit body exceeds size limit", false, nil)
	}
	if p.maxAttributeCount > 0 && record.AttributesLen() > p.maxAttributeCount {
		return newAuditStatusError(AuditErrorPayloadTooLarge, "audit attributes exceed count limit", false, nil)
	}
	if p.rateLimiter != nil && !p.rateLimiter.Allow() {
		return newAuditStatusError(AuditErrorTooManyRequests, "audit rate limit exceeded", true, nil)
	}
	return nil
}

func NewAuditLoggerProviderWithProcessor(processor *AuditLogProcessor) *AuditLoggerProvider {
	if processor == nil {
		return NewAuditLoggerProvider()
	}
	return NewAuditLoggerProvider(WithAuditRecordProcessor(processor))
}

type AuditRecord struct {
	Record
	EventName string
	Actor     log.Value
	ActorType string
	Action    string
	Resource   log.Value
	TargetID   string
	TargetType string
	Outcome    string
	SourceIP  string
	RecordID  string
	Hash      string
	Signature string
	HMAC      string
	IntegrityValue       string
	IntegrityAlgorithm   string
	IntegrityCertificate string
	SourceType           string
	SchemaVersion string
	SignContent   string
	HashAlgorithm string
	KeyID string
	SequenceNo int64
	PrevHash string
}
