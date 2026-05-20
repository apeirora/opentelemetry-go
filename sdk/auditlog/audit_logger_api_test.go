// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

func TestEmitWithResultPolicyUnauthorized(t *testing.T) {
	provider := NewAuditLoggerProvider(policyTestProviderOpts(
		WithAuditAuthorizer(func(ctx context.Context, record AuditRecord) error {
			return newAuditStatusError(AuditErrorUnauthorized, "missing credentials", false, nil)
		}),
	)...)
	logger := provider.Logger("test")
	result := logger.EmitWithResult(context.Background(), newValidAuditRecord("body"))
	if result.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", result.StatusCode)
	}
}

func TestEmitWithResultPolicyForbiddenOnGenericAuthorizerError(t *testing.T) {
	provider := NewAuditLoggerProvider(policyTestProviderOpts(
		WithAuditAuthorizer(func(ctx context.Context, record AuditRecord) error {
			return errors.New("denied")
		}),
	)...)
	logger := provider.Logger("test")
	result := logger.EmitWithResult(context.Background(), newValidAuditRecord("body"))
	if result.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", result.StatusCode)
	}
}

func TestEmitWithResultPolicyPayloadTooLarge(t *testing.T) {
	provider := NewAuditLoggerProvider(policyTestProviderOpts(
		WithAuditMaxBodyBytes(4),
	)...)
	logger := provider.Logger("test")
	result := logger.EmitWithResult(context.Background(), newValidAuditRecord("too-large-body"))
	if result.StatusCode != 413 {
		t.Fatalf("expected 413, got %d", result.StatusCode)
	}
}

func TestEmitWithResultPolicyAttributeCountTooLarge(t *testing.T) {
	provider := NewAuditLoggerProvider(policyTestProviderOpts(
		WithAuditMaxAttributeCount(1),
	)...)
	logger := provider.Logger("test")
	record := newValidAuditRecord("body")
	record.AddAttributes(log.String("extra", "v"))
	result := logger.EmitWithResult(context.Background(), record)
	if result.StatusCode != 413 {
		t.Fatalf("expected 413, got %d", result.StatusCode)
	}
}

func TestEmitWithResultPolicyRateLimited(t *testing.T) {
	provider := NewAuditLoggerProvider(policyTestProviderOpts(
		WithAuditMaxRequestsPerSecond(1),
	)...)
	logger := provider.Logger("test")
	first := logger.EmitWithResult(context.Background(), newValidAuditRecord("body-1"))
	second := logger.EmitWithResult(context.Background(), newValidAuditRecord("body-2"))
	if first.StatusCode == 429 {
		t.Fatalf("first request should not be rate limited")
	}
	if second.StatusCode != 429 {
		t.Fatalf("expected second status 429, got %d", second.StatusCode)
	}
}

const policyTestHMACKey = "policy-test-hmac-key"

func policyTestProviderOpts(extra ...AuditLoggerProviderOption) []AuditLoggerProviderOption {
	opts := []AuditLoggerProviderOption{
		WithAuditHMACVerificationKey([]byte(policyTestHMACKey)),
		WithAuditHashAlgorithm("sha256"),
	}
	return append(opts, extra...)
}

func newValidAuditRecord(body string) AuditRecord {
	record := Record{}
	record.SetTimestamp(time.Now().UTC())
	record.SetBody(log.StringValue(body))
	record.AddAttributes(log.String("base", "value"))
	return AuditRecord{
		Record:        record,
		EventName:     "user.login",
		Actor:         log.StringValue("actor"),
		ActorType:     "user",
		Action:        "login",
		Resource:      log.StringValue("resource"),
		Outcome:       "success",
		RecordID:      "record-1",
		SchemaVersion: "1.0",
	}
}
