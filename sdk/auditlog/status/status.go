// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package status

import "fmt"

type ErrorCode string

const (
	ErrorInvalidRequest  ErrorCode = "invalid_request"
	ErrorUnauthorized    ErrorCode = "unauthorized"
	ErrorForbidden       ErrorCode = "forbidden"
	ErrorConflict        ErrorCode = "conflict"
	ErrorPayloadTooLarge ErrorCode = "payload_too_large"
	ErrorTooManyRequests ErrorCode = "too_many_requests"
	ErrorUnavailable     ErrorCode = "unavailable"
)

type Error struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(code ErrorCode, msg string, retryable bool, cause error) error {
	return &Error{
		Code:      code,
		Message:   msg,
		Retryable: retryable,
		Cause:     cause,
	}
}

func Map(err error) (int, string, string) {
	if err == nil {
		return 200, "delivered", ""
	}
	statusErr, ok := err.(*Error)
	if !ok {
		return 503, "rejected", err.Error()
	}
	switch statusErr.Code {
	case ErrorInvalidRequest:
		return 400, "rejected", statusErr.Error()
	case ErrorUnauthorized:
		return 401, "rejected", statusErr.Error()
	case ErrorForbidden:
		return 403, "rejected", statusErr.Error()
	case ErrorConflict:
		return 409, "rejected", statusErr.Error()
	case ErrorPayloadTooLarge:
		return 413, "rejected", statusErr.Error()
	case ErrorTooManyRequests:
		return 429, "rejected", statusErr.Error()
	case ErrorUnavailable:
		return 503, "rejected", statusErr.Error()
	default:
		return 503, "rejected", statusErr.Error()
	}
}
