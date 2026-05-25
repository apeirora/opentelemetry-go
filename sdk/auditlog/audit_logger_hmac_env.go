// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"fmt"
	"os"
	"strings"
)

const (
	// EnvAuditlogHMACKeyFile is the name of the environment variable that holds a path to a
	// file containing the HMAC verification key. The file contents are trimmed of surrounding
	// ASCII whitespace. When set to a non-empty path, it takes precedence over [EnvAuditlogHMACKey].
	EnvAuditlogHMACKeyFile = "OTEL_AUDITLOG_HMAC_KEY_FILE"

	// EnvAuditlogHMACKey is the name of the environment variable that holds the raw HMAC
	// verification key string (trimmed of surrounding ASCII whitespace).
	EnvAuditlogHMACKey = "OTEL_AUDITLOG_HMAC_KEY"
)

// HMACVerificationKeyFromEnvironment loads the HMAC verification key from the process environment.
// If [EnvAuditlogHMACKeyFile] is set to a non-empty path, the key is read from that file; otherwise,
// if [EnvAuditlogHMACKey] is non-empty after trimming, that value is used. If neither yields a key,
// it returns nil, nil.
func HMACVerificationKeyFromEnvironment() ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(EnvAuditlogHMACKeyFile))
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("auditlog: read %s (%q): %w", EnvAuditlogHMACKeyFile, path, err)
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return nil, fmt.Errorf("auditlog: %s file %q is empty after trim", EnvAuditlogHMACKeyFile, path)
		}
		return []byte(key), nil
	}
	keyStr := strings.TrimSpace(os.Getenv(EnvAuditlogHMACKey))
	if keyStr != "" {
		return []byte(keyStr), nil
	}
	return nil, nil
}

// WithAuditHMACVerificationKeyFromEnvironment configures the HMAC verification key from the
// process environment when [HMACVerificationKeyFromEnvironment] succeeds. On failure or when no
// key is configured, this option is a no-op; use [HMACVerificationKeyFromEnvironment] directly
// when load errors must be handled explicitly.
func WithAuditHMACVerificationKeyFromEnvironment() AuditLoggerProviderOption {
	key, err := HMACVerificationKeyFromEnvironment()
	if err != nil || len(key) == 0 {
		return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
			return cfg
		})
	}
	return WithAuditHMACVerificationKey(key)
}
