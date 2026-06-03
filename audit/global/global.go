// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package global

import (
	"sync"

	"go.opentelemetry.io/otel/audit"
)

var (
	mu       sync.RWMutex
	provider audit.AuditProvider
)

// SetAuditProvider sets the global audit provider.
func SetAuditProvider(p audit.AuditProvider) {
	mu.Lock()
	defer mu.Unlock()
	provider = p
}

// GetAuditProvider returns the global audit provider, or nil if unset.
func GetAuditProvider() audit.AuditProvider {
	mu.RLock()
	defer mu.RUnlock()
	return provider
}
