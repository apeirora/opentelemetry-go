// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlpexport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

func normalizeHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return defaultVerifyEndpoint
	}
	if strings.Contains(endpoint, "://") {
		u, err := url.Parse(endpoint)
		if err == nil && u.Host != "" {
			return u.Host
		}
	}
	return endpoint
}

func verifyTLSAtStartup(ctx context.Context, settings verifySettings) error {
	settings = settings.resolved()
	if !settings.startupVerify || settings.insecure {
		return nil
	}

	timeout := settings.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}

	host := normalizeHost(settings.endpoint)
	cfg := cloneTLSConfig(settings.tlsCfg, host)
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", host, cfg)
	if err == nil {
		_ = conn.Close()
		return nil
	}
	if isBenignCollectorUnreachable(err) {
		return nil
	}
	return fmt.Errorf("otlp tls verification failed for %s: %w", host, err)
}

func cloneTLSConfig(holder *tlsConfigHolder, host string) *tls.Config {
	var base *tls.Config
	if holder != nil && holder.cfg != nil {
		base = holder.cfg.Clone()
	} else {
		base = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if base.ServerName == "" {
		base.ServerName = tlsServerName(host)
	}
	if base.MinVersion == 0 {
		base.MinVersion = tls.VersionTLS12
	}
	return base
}

func tlsServerName(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return h
}

func isBenignCollectorUnreachable(err error) bool {
	for err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			if opErr.Op == "dial" {
				return true
			}
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
