// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import "fmt"

type AuditErrorCode string

const (
	AuditErrorInvalidRequest  AuditErrorCode = "invalid_request"
	AuditErrorUnauthorized    AuditErrorCode = "unauthorized"
	AuditErrorForbidden       AuditErrorCode = "forbidden"
	AuditErrorConflict        AuditErrorCode = "conflict"
	AuditErrorPayloadTooLarge AuditErrorCode = "payload_too_large"
	AuditErrorTooManyRequests AuditErrorCode = "too_many_requests"
	AuditErrorUnavailable     AuditErrorCode = "unavailable"
)

type AuditStatusError struct {
	Code      AuditErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *AuditStatusError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AuditStatusError) Unwrap() error {
	return e.Cause
}

func newAuditStatusError(code AuditErrorCode, msg string, retryable bool, cause error) error {
	return &AuditStatusError{
		Code:      code,
		Message:   msg,
		Retryable: retryable,
		Cause:     cause,
	}
}

func mapAuditError(err error) (int, string, string) {
	if err == nil {
		return 200, "delivered", ""
	}
	statusErr, ok := err.(*AuditStatusError)
	if !ok {
		return 503, "rejected", err.Error()
	}
	switch statusErr.Code {
	case AuditErrorInvalidRequest:
		return 400, "rejected", statusErr.Error()
	case AuditErrorUnauthorized:
		return 401, "rejected", statusErr.Error()
	case AuditErrorForbidden:
		return 403, "rejected", statusErr.Error()
	case AuditErrorConflict:
		return 409, "rejected", statusErr.Error()
	case AuditErrorPayloadTooLarge:
		return 413, "rejected", statusErr.Error()
	case AuditErrorTooManyRequests:
		return 429, "rejected", statusErr.Error()
	case AuditErrorUnavailable:
		return 503, "rejected", statusErr.Error()
	default:
		return 503, "rejected", statusErr.Error()
	}
}
