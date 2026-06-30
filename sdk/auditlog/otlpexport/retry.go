// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport

import (
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
)

func defaultAuditHTTPRetryOption() otlploghttp.Option {
	return otlploghttp.WithRetry(otlploghttp.RetryConfig{
		Enabled:         true,
		InitialInterval: 200 * time.Millisecond,
		MaxInterval:     500 * time.Millisecond,
		MaxElapsedTime:  750 * time.Millisecond,
	})
}

func (c *buildConfig) appendHTTPRetryOption() {
	if c.httpRetrySet && !c.httpRetryEnabled {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
		return
	}
	if !c.httpRetrySet {
		c.otlpOpts = append(c.otlpOpts, defaultAuditHTTPRetryOption())
	}
}
