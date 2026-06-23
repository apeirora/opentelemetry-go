// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "context"

// StartupExporterVerifier is implemented by exporters that can validate transport
// configuration (for example TLS) before the processor accepts traffic.
type StartupExporterVerifier interface {
	VerifyStartup(ctx context.Context) error
}
