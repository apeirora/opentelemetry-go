// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestIsExportConnectionFailure(t *testing.T) {
	connRefused := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	http503 := fmt.Errorf("failed to send logs to http://localhost:4318/v1/audit: 503 Service Unavailable (body: (empty))")
	httpRetryable := fmt.Errorf("retry-able request failure: %w", fmt.Errorf("body: (empty)"))
	partialSuccess := errors.New("audit: export failed (partial_success not allowed): OTLP partial success")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "connection refused", err: connRefused, want: true},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "http 503", err: http503, want: false},
		{name: "retryable http body", err: httpRetryable, want: false},
		{name: "partial success", err: partialSuccess, want: false},
		{name: "generic", err: errors.New("export blocked"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExportConnectionFailure(tc.err); got != tc.want {
				t.Fatalf("isExportConnectionFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
