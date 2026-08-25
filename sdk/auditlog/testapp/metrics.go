// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type metricsServer struct {
	addr string
}

func startMetricsServer(addr string) (*metricsServer, error) {
	reg := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "testapp: metrics server: %v\n", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "testapp: SDK metrics on http://%s/metrics (custom registry; no collector metrics)\n", addr)
	return &metricsServer{addr: addr}, nil
}

func (m *metricsServer) scrapeHint() string {
	if m == nil {
		return ""
	}
	host := m.addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	return fmt.Sprintf(`curl.exe -s http://%s/metrics | Select-String 'otel_scope_name="go.opentelemetry.io/otel/sdk/auditlog"'`, host)
}
