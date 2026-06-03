// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package audit

import "context"

// AuditProvider is the entry point of the Audit Logging API.
// Implementations must be safe for concurrent use and must not expose sampler configuration.
type AuditProvider interface {
	Logger(name string, opts ...LoggerOption) AuditLogger
	Shutdown(ctx context.Context) error
	ForceFlush(ctx context.Context) error
}
