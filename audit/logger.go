// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package audit

import "context"

// AuditLogger emits audit records. Implementations must be safe for concurrent use.
type AuditLogger interface {
	// Emit submits an AuditRecord to the audit pipeline.
	// It blocks until the audit sink acknowledges the record (or returns a non-nil error).
	// Emit must not return a zero receipt with a nil error.
	Emit(ctx context.Context, record AuditRecord) (AuditReceipt, error)
}

// LoggerOption configures an AuditLogger.
type LoggerOption interface {
	applyLogger(*loggerConfig)
}

type loggerConfig struct {
	version   string
	schemaURL string
}

type loggerOptionFunc func(*loggerConfig)

func (f loggerOptionFunc) applyLogger(c *loggerConfig) { f(c) }

// WithLoggerVersion sets the logger version (diagnostic only; not InstrumentationScope).
func WithLoggerVersion(version string) LoggerOption {
	return loggerOptionFunc(func(c *loggerConfig) { c.version = version })
}

// WithLoggerSchemaURL sets the logger schema URL (diagnostic only).
func WithLoggerSchemaURL(schemaURL string) LoggerOption {
	return loggerOptionFunc(func(c *loggerConfig) { c.schemaURL = schemaURL })
}

// LoggerConfig holds resolved logger options.
type LoggerConfig struct {
	Version   string
	SchemaURL string
}

// ApplyLoggerOptions resolves logger options.
func ApplyLoggerOptions(opts ...LoggerOption) LoggerConfig {
	c := &loggerConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyLogger(c)
		}
	}
	return LoggerConfig{Version: c.version, SchemaURL: c.schemaURL}
}
