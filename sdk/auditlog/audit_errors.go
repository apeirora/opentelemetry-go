// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "go.opentelemetry.io/otel/sdk/auditlog/status"

type AuditErrorCode = status.ErrorCode

const (
	AuditErrorInvalidRequest  AuditErrorCode = status.ErrorInvalidRequest
	AuditErrorUnauthorized    AuditErrorCode = status.ErrorUnauthorized
	AuditErrorForbidden       AuditErrorCode = status.ErrorForbidden
	AuditErrorConflict        AuditErrorCode = status.ErrorConflict
	AuditErrorPayloadTooLarge AuditErrorCode = status.ErrorPayloadTooLarge
	AuditErrorTooManyRequests AuditErrorCode = status.ErrorTooManyRequests
	AuditErrorUnavailable     AuditErrorCode = status.ErrorUnavailable
)

type AuditStatusError = status.Error

func newAuditStatusError(code AuditErrorCode, msg string, retryable bool, cause error) error {
	return status.New(code, msg, retryable, cause)
}

func mapAuditError(err error) (int, string, string) {
	return status.Map(err)
}
