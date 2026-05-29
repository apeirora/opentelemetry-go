# Audit Log SDK Package

`go.opentelemetry.io/otel/sdk/auditlog` provides audit-focused log processing on top of `sdk/log`.

It includes:

- an `AuditLogProcessor` with FIFO queueing, periodic export, and retry
- pluggable `AuditLogStore` implementations
- a storage-extension adapter for memory/file/Redis/SQL backends
- an `AuditLoggerProvider`/`AuditLogger` API with policy checks and integrity verification

## Package Layout

- `sdk/auditlog`: public package surface (package name `log`)
- `sdk/auditlog/store`: file and in-memory `AuditLogStore` implementations
- `sdk/auditlog/storage`: storage extension and backend clients
- `sdk/auditlog/identity`: record identity helpers (`record_id`, hash)
- `sdk/auditlog/status`: audit status/error mapping

## OTLP HTTP export

`Exporter` is whatever you provide; this package does not configure OTLP endpoints. The standard `otlploghttp` client defaults to URL path `/v1/logs`. Collectors that ingest audit logs on `/auditlogs` need `otlploghttp.WithURLPath("/auditlogs")` or a URL whose path is `/auditlogs`. The runnable demo under `sdk/auditlog/testapp` sets `/auditlogs` when `-otlp-endpoint` has no path (for example `http://localhost:4318`).

## Core Types

### `AuditLogStore`

`AuditLogStore` is the persistence contract used by `AuditLogProcessor`:

```go
type AuditLogStore interface {
	Save(ctx context.Context, record *sdklog.Record) error
	RemoveAll(ctx context.Context, records []sdklog.Record) error
	GetAll(ctx context.Context) ([]sdklog.Record, error)
}
```

### `AuditLogProcessor`

`AuditLogProcessor`:

- stores records before export
- loads persisted records on startup
- orders queued records FIFO (first enqueued, first exported)
- exports in batches on a background ticker
- retries failed exports using configured backoff
- supports `ForceFlush` and `Shutdown`

Configuration is represented by `AuditLogProcessorConfig`.

### `AuditLoggerProvider` and `AuditLogger`

`AuditLoggerProvider` builds and manages `AuditLogger` instances.
`AuditLogger` accepts `AuditRecord` and can return a detailed `AuditEmitResult` via `EmitWithResult`.

Provider options support:

- processor registration (`WithAuditRecordProcessor`)
- integrity configuration (`WithAuditHMACVerificationKey`, `WithAuditSignatureVerifier`, `WithAuditHashAlgorithm`)
- policy enforcement (`WithAuditAuthorizer`, `WithAuditMaxBodyBytes`, `WithAuditMaxAttributeCount`, `WithAuditMaxRequestsPerSecond`)

`AuditLogger` validates required fields, verifies hash/signature/HMAC, applies policy checks, emits via registered processors, and reports status details.

## Building an `AuditLogProcessor`

Use `NewAuditLogProcessorBuilder(exporter, store)` when you already have a store:

```go
builder := NewAuditLogProcessorBuilder(exporter, store).
	SetScheduleDelay(1 * time.Second).
	SetMaxExportBatchSize(512).
	SetExporterTimeout(30 * time.Second).
	SetRetryPolicy(RetryPolicy{
		InitialBackoff:    time.Second,
		MaxBackoff:        time.Minute,
		BackoffMultiplier: 2.0,
	}).
	SetWaitOnExport(false)

processor, err := builder.Build()
```

Available builder setters:

- `SetExceptionHandler`
- `SetScheduleDelay`
- `SetMaxExportBatchSize`
- `SetExporterTimeout`
- `SetRetryPolicy`
- `SetWaitOnExport`
- `SetDeliveryMode`
- `SetStorageWriteMode`

`BuildOrPanic`, `GetConfig`, and `ValidateConfig` are also available.

## Storage Options

### 1) Direct stores

- `NewAuditLogFileStore(path)` (JSON records persisted to file)
- `NewAuditLogInMemoryStore()` (in-memory, useful for tests)

### 2) Builder-managed storage extension

Use `NewAuditLogProcessorWithStorage(exporter)` and choose backend:

- `WithMemoryStorage()`
- `WithFileStorage(directory)`
- `WithRedisStorage(...)`
- `WithSQLStorage(...)`

Example:

```go
processor, err := NewAuditLogProcessorWithStorage(exporter).
	WithRedisStorage(
		WithRedisEndpoint("localhost:6379"),
		WithRedisKeyPrefix("otel_audit_"),
		WithRedisKeyExpiration(24*time.Hour),
	).
	Build()
```

### 3) Manual storage extension + adapter

If you need direct extension control:

```go
ext, err := NewStorageExtension(WithMemoryStorage())
if err != nil {
	return err
}
if err := ext.Start(ctx); err != nil {
	return err
}
client, err := ext.GetClient(ctx, "audit_processor")
if err != nil {
	return err
}
adapter, err := NewAuditLogStorageExtensionAdapter(client)
if err != nil {
	return err
}
```

`adapter` satisfies `AuditLogStore` and can be used in `NewAuditLogProcessorBuilder`.

## Delivery Mode

`AuditLogProcessorConfig.DeliveryMode` controls how records are delivered:

- `AuditDeliveryModeAsyncStoreRetry` (default): save to store first, queue, and export in background with retries.
- `AuditDeliveryModeSyncDirect`: send directly to exporter on `OnEmit` without queue/store persistence.

`AuditLogProcessorConfig.StorageWriteMode` controls when records are written to store in async mode:

- `AuditStorageWriteAlways` (default): save before export attempt. Survives process restarts via the configured backend.
- `AuditStorageWriteOnError`: save only when an export attempt fails. Lower write load while healthy; records in the in-memory queue are lost if the process crashes before export fails or succeeds.

Choose a storage backend when building the processor:

| Backend | Implementation | Notes |
|---------|----------------|-------|
| Memory | `SimpleKeyValueStorageClient` | Process-local; not durable |
| File | `BoltDBStorageClient` (bbolt) or `AuditLogFileStore` (JSONL) | Durable on disk |
| Redis | `RealRedisStorageClient` | Distributed; optional TTL via `Expiration` (0 = no expiry) |
| SQL | `SQLStorageClient` | `database/sql` with sqlite (built-in), postgres, or mysql drivers |

For SQLite the `sqlite` driver is registered when importing `go.opentelemetry.io/otel/sdk/auditlog/storage`. For PostgreSQL or MySQL, register the driver in your application (for example `_ "github.com/lib/pq"` or `_ "github.com/go-sql-driver/mysql"`).

Example:

```go
processor, err := NewAuditLogProcessorWithStorage(exporter).
	SetDeliveryMode(AuditDeliveryModeAsyncStoreRetry).
	Build()

syncProcessor, err := NewAuditLogProcessorBuilder(exporter, NewAuditLogInMemoryStore()).
	SetDeliveryMode(AuditDeliveryModeSyncDirect).
	Build()
```

## Default Processor Configuration

`DefaultAuditLogProcessorConfig(exporter, store)` sets:

- `ScheduleDelay`: `1s`
- `MaxExportBatchSize`: `512`
- `ExporterTimeout`: `30s`
- `RetryPolicy.InitialBackoff`: `1s`
- `RetryPolicy.MaxBackoff`: `1m`
- `RetryPolicy.BackoffMultiplier`: `2.0`
- `WaitOnExport`: `false`
- `ExceptionHandler`: `DefaultAuditExceptionHandler`

## Audit Record Requirements

`AuditLogger` validates that `AuditRecord` includes:

- timestamp
- event name
- actor and actor type
- action
- resource
- outcome
- body
- at least one attribute
- `record_id`
- `hash`
- `signature` or `hmac`
- `schema_version`

Integrity verification supports hash and optional HMAC/signature validation.

## Basic End-to-End Example

```go
store, err := NewAuditLogFileStore("./audit.log")
if err != nil {
	return err
}

processor, err := NewAuditLogProcessorBuilder(exporter, store).Build()
if err != nil {
	return err
}
defer processor.Shutdown(context.Background())

provider := NewAuditLoggerProvider(
	WithAuditRecordProcessor(processor),
)
defer provider.Shutdown(context.Background())

logger := provider.Logger("security-audit")

record := AuditRecord{
	Record: Record{},
	EventName: "user.login",
	Actor: log.StringValue("user-123"),
	ActorType: "user",
	Action: "login",
	Resource: log.StringValue("session"),
	Outcome: "success",
	RecordID: "evt-001",
	HMAC: "<hmac-hex>",
	SchemaVersion: "1.0",
}
record.SetTimestamp(time.Now())
record.SetObservedTimestamp(time.Now())
record.SetBody(log.StringValue(`{"result":"ok"}`))
record.AddAttributes(log.String("tenant.id", "acme"))

result := logger.EmitWithResult(context.Background(), record)
_ = result
```

## Error Model

Audit errors are represented with status-aligned codes in `audit_errors.go` (`AuditErrorInvalidRequest`, `AuditErrorForbidden`, `AuditErrorConflict`, `AuditErrorTooManyRequests`, `AuditErrorUnavailable`, and others).

Processor-side failures are surfaced through `AuditExceptionHandler`.

## Related Examples

See:

- `sdk/auditlog/example_usage.go`
- `sdk/auditlog/example_simple_storage.go`
- `sdk/auditlog/example_storage_extension_usage.go`

# Audit Log Storage for OpenTelemetry Go SDK

This package provides audit log storage capabilities for the OpenTelemetry Go SDK, similar to the Java implementation. It includes persistent storage, FIFO export ordering, and retry mechanisms for reliable audit logging.

## Features

- **Persistent Storage**: File-based storage that survives application restarts
- **FIFO Processing**: Records are exported in arrival order
- **Retry Logic**: Automatic retry with exponential backoff for failed exports
- **Batch Processing**: Configurable batch sizes for efficient export
- **Thread-Safe**: Concurrent access support with proper locking
- **Exception Handling**: Customizable error handling for audit failures
- **Memory Store**: In-memory implementation for testing and development
- **Storage Extensions**: Compatible with OpenTelemetry Collector storage backends (File, Redis, SQL, custom)

## Components

### AuditLogStore Interface

The `AuditLogStore` interface provides the core storage functionality:

```go
type AuditLogStore interface {
    Save(ctx context.Context, record *Record) error
    RemoveAll(ctx context.Context, records []Record) error
    GetAll(ctx context.Context) ([]Record, error)
}
```

### Implementations

#### Legacy Implementations

##### AuditLogFileStore

File-based persistent storage that writes audit logs to disk:

```go
store, err := NewAuditLogFileStore("/path/to/audit/logs")
if err != nil {
    log.Fatal(err)
}
```

##### AuditLogInMemoryStore

In-memory storage for testing and development:

```go
store := NewAuditLogInMemoryStore()
```

#### Storage Extension (Recommended)

The new Storage Extension pattern provides compatibility with OpenTelemetry Collector storage backends and supports multiple storage types:

##### Simple Key-Value Storage (In-Memory)

```go
client := NewSimpleKeyValueStorageClient()
adapter, err := NewAuditLogStorageExtensionAdapter(client)
```

##### BoltDB Storage (File-Based)

```go
client, err := NewBoltDBStorageClient("/var/log/audit/storage.db")
adapter, err := NewAuditLogStorageExtensionAdapter(client)
```

##### Redis Storage (Distributed)

```go
config := RedisStorageConfig{
    Endpoint:   "localhost:6379",
    Password:   "",
    DB:         0,
    Prefix:     "audit_",
    Expiration: 24 * time.Hour,
}
client, err := NewRedisStorageClient(config)
adapter, err := NewAuditLogStorageExtensionAdapter(client)
```

##### SQL Database Storage

```go
config := SQLStorageConfig{
    Driver:     "postgres",
    Datasource: "postgresql://user:pass@localhost/auditdb",
    TableName:  "audit_logs",
}
client, err := NewSQLStorageClient(config)
adapter, err := NewAuditLogStorageExtensionAdapter(client)
```

**For detailed information about storage, see:**
- [📖 Storage Guide](../../examples/audit-log-stress-test/docs/START_HERE.md) - Quick start examples
- [User Guide](../../examples/audit-log-stress-test/docs/USER_GUIDE_SIMPLE_STORAGE.md) - Complete guide
- [Quick Start API](../../examples/audit-log-stress-test/docs/QUICK_START_SIMPLE_API.md) - All storage options

### AuditLogProcessor

The `AuditLogProcessor` implements the `Processor` interface and provides:

- FIFO export ordering (enqueue order preserved)
- Automatic retry with exponential backoff
- Batch processing with configurable sizes
- Background processing with periodic exports
- Graceful shutdown with remaining log export

### Configuration

Use the `AuditLogProcessorBuilder` to configure the processor:

```go
processor, err := NewAuditLogProcessorBuilder(exporter, store).
    SetScheduleDelay(100 * time.Millisecond).
    SetMaxExportBatchSize(512).
    SetExporterTimeout(30 * time.Second).
    SetRetryPolicy(RetryPolicy{
        MaxAttempts:      3,
        InitialBackoff:   time.Second,
        MaxBackoff:       time.Minute,
        BackoffMultiplier: 2.0,
    }).
    SetWaitOnExport(false).
    Build()
```

## Usage Examples

### Basic Usage

```go
package main

import (
    "context"
    "log"
    "time"
    
    "go.opentelemetry.io/otel/log"
    "go.opentelemetry.io/otel/sdk/auditlog"
)

func main() {
    // Create audit log store
    store, err := log.NewAuditLogFileStore("/var/log/audit")
    if err != nil {
        log.Fatal(err)
    }
    
    // Create exporter (your actual exporter implementation)
    exporter := yourExporter
    
    // Create processor
    processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
        SetScheduleDelay(time.Second).
        SetMaxExportBatchSize(100).
        Build()
    if err != nil {
        log.Fatal(err)
    }
    defer processor.Shutdown(context.Background())
    
    // Create logger with audit processor
    logger := log.NewLogger(processor)
    
    // Log audit events
    record := &log.Record{
        Timestamp: time.Now(),
        Severity:  log.SeverityInfo,
        Body:      log.StringValue("User login attempt"),
    }
    record.AddAttributes(log.String("user_id", "12345"))
    
    if err := logger.Emit(context.Background(), record); err != nil {
        log.Printf("Failed to emit audit log: %v", err)
    }
}
```

### Custom Exception Handling

```go
type CustomAuditHandler struct {
    alertService AlertService
}

func (h *CustomAuditHandler) Handle(exception *log.AuditException) {
    // Send alert for audit failures
    h.alertService.SendAlert(fmt.Sprintf(
        "Audit log export failed: %s", exception.Message))
    
    // Log the exception
    log.Printf("Audit exception: %v", exception)
}

// Use in processor
processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
    SetExceptionHandler(&CustomAuditHandler{alertService: alerts}).
    Build()
```

### Testing with In-Memory Store

```go
func TestAuditLogging(t *testing.T) {
    store := log.NewAuditLogInMemoryStore()
    exporter := NewMockExporter()
    
    processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
        SetScheduleDelay(10 * time.Millisecond).
        Build()
    if err != nil {
        t.Fatal(err)
    }
    defer processor.Shutdown(context.Background())
    
    // Test audit logging
    record := createTestAuditRecord("test event", log.SeverityInfo)
    if err := processor.OnEmit(context.Background(), &record); err != nil {
        t.Fatalf("Failed to emit record: %v", err)
    }
    
    // Wait for processing
    time.Sleep(50 * time.Millisecond)
    
    // Verify export
    if exporter.GetExportCount() == 0 {
        t.Error("Expected record to be exported")
    }
}
```

## Configuration Options

### AuditLogProcessorConfig

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ScheduleDelay` | `time.Duration` | `1s` | Delay between periodic exports |
| `MaxExportBatchSize` | `int` | `512` | Maximum records per export batch |
| `ExporterTimeout` | `time.Duration` | `30s` | Timeout for export operations |
| `WaitOnExport` | `bool` | `false` | Whether to wait for exports to complete |
| `RetryPolicy` | `RetryPolicy` | See below | Retry configuration |

### RetryPolicy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `MaxAttempts` | `int` | `0` (unlimited) | Maximum export retry cycles after a failed batch; records remain in the store (or queue if not yet persisted) when exceeded |
| `InitialBackoff` | `time.Duration` | `1s` | Initial backoff duration |
| `MaxBackoff` | `time.Duration` | `1m` | Maximum backoff duration |
| `BackoffMultiplier` | `float64` | `2.0` | Backoff multiplier |

## Export Ordering

The async processor exports records in FIFO order (first enqueued, first exported) so receivers observe a stable time sequence.

## File Storage Format

Audit logs are stored as JSON lines, one record per line:

```json
{"timestamp":"2023-12-07T10:30:00Z","severity":"INFO","body":"User login","attributes":[{"key":"user_id","value":"12345"}]}
{"timestamp":"2023-12-07T10:31:00Z","severity":"ERROR","body":"Authentication failed","attributes":[{"key":"user_id","value":"67890"}]}
```

## Error Handling

The audit log processor includes comprehensive error handling:

- **Storage Errors**: Automatically handled with retry logic
- **Export Errors**: Retry with exponential backoff
- **Configuration Errors**: Validation with clear error messages
- **Shutdown Errors**: Graceful handling of shutdown scenarios

## Performance Considerations

- **Batch Processing**: Reduces overhead by processing multiple records together
- **FIFO queue**: Stable export ordering by enqueue time
- **Async Processing**: Non-blocking log emission with background export
- **File I/O**: Optimized for append-only operations
- **Memory Usage**: Configurable batch sizes to control memory consumption

## Security Considerations

- **File Permissions**: Ensure audit log files have appropriate permissions
- **Disk Space**: Monitor disk usage for audit log files
- **Retention**: Implement log rotation and cleanup policies
- **Encryption**: Consider encrypting audit log files at rest
- **Access Control**: Restrict access to audit log files and directories

## Migration from Java Implementation

This Go implementation provides equivalent functionality to the Java audit log implementation:

| Java Component | Go Equivalent |
|----------------|---------------|
| `AuditLogStore` | `AuditLogStore` interface |
| `AuditLogFileStore` | `AuditLogFileStore` |
| `AuditLogRecordProcessor` | `AuditLogProcessor` |
| `AuditException` | `AuditException` |
| `AuditExceptionHandler` | `AuditExceptionHandler` |
| `RetryPolicy` | `RetryPolicy` |

## Contributing

When contributing to the audit log functionality:

1. Add tests for new features
2. Update documentation
3. Ensure thread safety
4. Follow Go best practices
5. Consider performance implications
6. Test with both file and in-memory stores

## License

This code is licensed under the Apache License 2.0, same as the OpenTelemetry project.
