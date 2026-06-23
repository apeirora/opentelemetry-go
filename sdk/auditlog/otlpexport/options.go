// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport

import (
	"crypto/tls"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
)

const (
	defaultVerifyEndpoint = "localhost:4318"
	defaultVerifyTimeout  = 10 * time.Second
)

type Option interface {
	apply(*buildConfig)
}

type optionFunc func(*buildConfig)

func (f optionFunc) apply(c *buildConfig) { f(c) }

type buildConfig struct {
	otlpOpts []otlploghttp.Option
	verify   verifySettings
}

type verifySettings struct {
	endpoint       string
	endpointSet    bool
	insecure       bool
	insecureSet    bool
	tlsCfg         *tlsConfigHolder
	timeout        time.Duration
	timeoutSet     bool
	startupVerify  bool
	startupVerifySet bool
}

type tlsConfigHolder struct {
	cfg *tls.Config
}

func (s verifySettings) resolved() verifySettings {
	out := s
	if !out.endpointSet {
		out.endpoint = defaultVerifyEndpoint
	}
	if !out.timeoutSet {
		out.timeout = defaultVerifyTimeout
	}
	if !out.startupVerifySet {
		out.startupVerify = !out.insecure
	}
	return out
}

func WithEndpoint(endpoint string) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithEndpoint(endpoint))
		c.verify.endpoint = normalizeHost(endpoint)
		c.verify.endpointSet = true
	})
}

func WithEndpointURL(rawURL string) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithEndpointURL(rawURL))
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			return
		}
		c.verify.endpoint = u.Host
		c.verify.endpointSet = true
		if u.Scheme == "http" {
			c.verify.insecure = true
			c.verify.insecureSet = true
		}
		if u.Scheme == "https" {
			c.verify.insecure = false
			c.verify.insecureSet = true
		}
	})
}

func WithInsecure() Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithInsecure())
		c.verify.insecure = true
		c.verify.insecureSet = true
	})
}

func WithURLPath(path string) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithURLPath(path))
	})
}

func WithHeaders(headers map[string]string) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithHeaders(headers))
	})
}

func WithTimeout(timeout time.Duration) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithTimeout(timeout))
		c.verify.timeout = timeout
		c.verify.timeoutSet = true
	})
}

func WithTLSClientConfig(tlsCfg *tls.Config) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, otlploghttp.WithTLSClientConfig(tlsCfg))
		c.verify.tlsCfg = &tlsConfigHolder{cfg: tlsCfg}
		c.verify.insecure = false
		c.verify.insecureSet = true
	})
}

func WithStartupVerify(enabled bool) Option {
	return optionFunc(func(c *buildConfig) {
		c.verify.startupVerify = enabled
		c.verify.startupVerifySet = true
	})
}

func otlpOnly(opt otlploghttp.Option) Option {
	return optionFunc(func(c *buildConfig) {
		c.otlpOpts = append(c.otlpOpts, opt)
	})
}
