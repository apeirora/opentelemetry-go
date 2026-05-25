module go.opentelemetry.io/otel/examples/audit-log-stress-test

go 1.25.0

require (
	go.opentelemetry.io/otel/log v0.19.0
	go.opentelemetry.io/otel/sdk/auditlog v0.14.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/log/logtest v0.19.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace go.opentelemetry.io/otel => ../..

replace go.opentelemetry.io/otel/sdk/auditlog => ../../sdk/auditlog

replace go.opentelemetry.io/otel/log => ../../log

replace go.opentelemetry.io/otel/metric => ../../metric

replace go.opentelemetry.io/otel/trace => ../../trace

replace go.opentelemetry.io/otel/sdk => ../../sdk
