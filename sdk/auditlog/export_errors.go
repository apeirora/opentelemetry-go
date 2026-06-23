// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

func isExportConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	if isExportHTTPResponseError(err) {
		return false
	}
	return hasUnderlyingConnectionFailure(err)
}

func isExportHTTPResponseError(err error) bool {
	for err != nil {
		msg := err.Error()
		if strings.HasPrefix(msg, "body:") {
			return true
		}
		if strings.Contains(msg, "failed to send logs to") {
			return true
		}
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "partial success") || strings.Contains(lower, "partial_success") {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func hasUnderlyingConnectionFailure(err error) bool {
	for err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return true
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return true
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			if urlErr.Timeout() || urlErr.Temporary() {
				return true
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
