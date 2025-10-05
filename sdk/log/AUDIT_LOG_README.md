# Audit Log Storage for OpenTelemetry Go SDK

This package provides audit log storage capabilities for the OpenTelemetry Go SDK, similar to the Java implementation. It includes persistent storage, priority-based processing, and retry mechanisms for reliable audit logging.

## Features

- **Persistent Storage**: File-based storage that survives application restarts
- **Priority Processing**: Higher severity logs are processed first
- **Retry Logic**: Automatic retry with exponential backoff for failed exports
- **Batch Processing**: Configurable batch sizes for efficient export
- **Thread-Safe**: Concurrent access support with proper locking
- **Exception Handling**: Customizable error handling for audit failures
- **Memory Store**: In-memory implementation for testing and development

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

#### AuditLogFileStore

File-based persistent storage that writes audit logs to disk:

```go
store, err := NewAuditLogFileStore("/path/to/audit/logs")
if err != nil {
    log.Fatal(err)
}
```

#### AuditLogInMemoryStore

In-memory storage for testing and development:

```go
store := NewAuditLogInMemoryStore()
```

### AuditLogProcessor

The `AuditLogProcessor` implements the `Processor` interface and provides:

- Priority-based processing (higher severity first)
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
    "go.opentelemetry.io/otel/sdk/log"
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
| `MaxAttempts` | `int` | `3` | Maximum retry attempts |
| `InitialBackoff` | `time.Duration` | `1s` | Initial backoff duration |
| `MaxBackoff` | `time.Duration` | `1m` | Maximum backoff duration |
| `BackoffMultiplier` | `float64` | `2.0` | Backoff multiplier |

## Priority Processing

Logs are processed in priority order based on severity:

1. **Fatal** (Priority 6) - Highest priority
2. **Error** (Priority 5)
3. **Warn** (Priority 4)
4. **Info** (Priority 3)
5. **Debug** (Priority 2)
6. **Trace** (Priority 1) - Lowest priority

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
- **Priority Queue**: Efficient processing of high-priority logs first
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
