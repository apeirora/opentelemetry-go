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

// WithAuditHMACVerificationKeyFromEnvironment configures the HMAC verification key from the
// process environment. If [EnvAuditlogHMACKeyFile] is set to a non-empty path, the key is read
// from that file; otherwise, if [EnvAuditlogHMACKey] is non-empty after trimming, that value
// is used. If neither yields a key, this option leaves any key from other options unchanged.
//
// Reading the key file or finding an empty file after trim causes a panic with a descriptive message.
func WithAuditHMACVerificationKeyFromEnvironment() AuditLoggerProviderOption {
	return auditLoggerProviderOptionFunc(func(cfg auditProviderConfig) auditProviderConfig {
		path := strings.TrimSpace(os.Getenv(EnvAuditlogHMACKeyFile))
		if path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				panic(fmt.Sprintf("auditlog: read %s (%q): %v", EnvAuditlogHMACKeyFile, path, err))
			}
			key := strings.TrimSpace(string(b))
			if key == "" {
				panic(fmt.Sprintf("auditlog: %s file %q is empty after trim", EnvAuditlogHMACKeyFile, path))
			}
			return WithAuditHMACVerificationKey([]byte(key)).apply(cfg)
		}
		keyStr := strings.TrimSpace(os.Getenv(EnvAuditlogHMACKey))
		if keyStr != "" {
			return WithAuditHMACVerificationKey([]byte(keyStr)).apply(cfg)
		}
		return cfg
	})
}
