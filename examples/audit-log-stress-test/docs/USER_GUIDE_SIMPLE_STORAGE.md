# Simple Storage API - User Guide

## End-User Focused API

Instead of manually creating storage extensions, clients, and adapters, you can now **choose your storage method directly when building the processor**. The SDK handles all the complexity internally.

## Quick Start

### 1. Memory Storage (Simplest)

Perfect for testing and development:

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithMemoryStorage().
    Build()
defer processor.Shutdown(ctx)
```

That's it! No configuration needed.

### 2. Redis Storage

For distributed systems:

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(
        sdklog.WithRedisEndpoint("localhost:6379"),
        sdklog.WithRedisAuth("password", 0),
        sdklog.WithRedisKeyPrefix("myapp_"),
        sdklog.WithRedisKeyExpiration(24*time.Hour),
    ).
    Build()
defer processor.Shutdown(ctx)
```

### 3. File Storage

For single-node production:

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithFileStorage("./storage").
    Build()
defer processor.Shutdown(ctx)
```

### 4. SQL Storage

For enterprise and compliance:

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithSQLStorage(
        sdklog.WithSQLDriver("postgres"),
        sdklog.WithSQLDatasource("postgresql://user:pass@localhost/db"),
        sdklog.WithSQLTable("audit_logs"),
    ).
    Build()
defer processor.Shutdown(ctx)
```

## Complete Example

```go
package main

import (
    "context"
    "time"
    
    "go.opentelemetry.io/otel/log"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
    ctx := context.Background()
    
    // Create your exporter
    exporter, _ := sdklog.NewOTLPExporter("http://localhost:4318")
    
    // Build processor with storage - ONE STEP!
    processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
        WithRedisStorage(
            sdklog.WithRedisEndpoint("localhost:6379"),
            sdklog.WithRedisKeyPrefix("audit_"),
        ).
        SetScheduleDelay(100 * time.Millisecond).
        SetMaxExportBatchSize(1000).
        Build()
    
    if err != nil {
        panic(err)
    }
    defer processor.Shutdown(ctx)
    
    // Emit logs
    record := &sdklog.Record{}
    record.SetBody(log.StringValue("User logged in"))
    record.AddAttributes(
        log.String("user_id", "12345"),
        log.String("action", "login"),
    )
    
    processor.OnEmit(ctx, record)
    processor.ForceFlush(ctx)
}
```

## Configuration Options

### Memory Storage

No options needed!

```go
.WithMemoryStorage()
```

### Redis Storage

```go
.WithRedisStorage(
    sdklog.WithRedisEndpoint("host:port"),           // Required
    sdklog.WithRedisAuth("password", 0),             // Optional: password and DB number
    sdklog.WithRedisKeyPrefix("myapp_"),            // Optional: key prefix (default: "otel_audit_")
    sdklog.WithRedisKeyExpiration(48*time.Hour),    // Optional: TTL (default: 24h)
)
```

### File Storage

```go
.WithFileStorage("./storage")  // Directory path
```

### SQL Storage

```go
.WithSQLStorage(
    sdklog.WithSQLDriver("postgres"),                        // Required: sqlite3, postgres, mysql
    sdklog.WithSQLDatasource("postgresql://..."),           // Required: connection string
    sdklog.WithSQLTable("audit_logs"),                      // Optional: table name
)
```

## Comparison: Old vs New API

### Old Way (Complex)

```go
// Step 1: Create extension
extension, _ := sdklog.NewStorageExtension(
    sdklog.WithRedisStorage("localhost:6379"),
)
extension.Start(ctx)

// Step 2: Get client
client, _ := extension.GetClient(ctx, "audit_processor")

// Step 3: Create adapter
adapter, _ := sdklog.NewAuditLogStorageExtensionAdapter(client)

// Step 4: Create processor
processor, _ := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
    Build()

// Step 5: Remember to shutdown extension!
defer extension.Shutdown(ctx)
defer processor.Shutdown(ctx)
```

### New Way (Simple)

```go
// ONE step - SDK handles everything!
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(
        sdklog.WithRedisEndpoint("localhost:6379"),
    ).
    Build()

// Just shutdown processor - extension cleanup is automatic!
defer processor.Shutdown(ctx)
```

## When to Use Each Storage Type

| Storage | When to Use | Pros | Cons |
|---------|------------|------|------|
| **Memory** | Testing, dev | Fast, simple | No persistence |
| **File** | Single-node prod | Persistent, no deps | Not distributed |
| **Redis** | Multi-node prod | Fast, distributed | Needs Redis |
| **SQL** | Enterprise | Queryable, ACID | Slower, complex |

## Environment-Based Configuration

```go
func createProcessor(exporter Exporter) (*sdklog.AuditLogProcessor, error) {
    builder := sdklog.NewAuditLogProcessorWithStorage(exporter)
    
    switch os.Getenv("STORAGE_TYPE") {
    case "redis":
        builder = builder.WithRedisStorage(
            sdklog.WithRedisEndpoint(os.Getenv("REDIS_ENDPOINT")),
            sdklog.WithRedisAuth(os.Getenv("REDIS_PASSWORD"), 0),
        )
    
    case "file":
        builder = builder.WithFileStorage(os.Getenv("STORAGE_DIR"))
    
    case "sql":
        builder = builder.WithSQLStorage(
            sdklog.WithSQLDriver(os.Getenv("SQL_DRIVER")),
            sdklog.WithSQLDatasource(os.Getenv("SQL_DATASOURCE")),
        )
    
    default:
        builder = builder.WithMemoryStorage()
    }
    
    return builder.Build()
}
```

## Error Handling

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(
        sdklog.WithRedisEndpoint("localhost:6379"),
    ).
    Build()

if err != nil {
    // Redis connection failed - handle gracefully
    log.Printf("Redis unavailable: %v, falling back to memory", err)
    
    processor, err = sdklog.NewAuditLogProcessorWithStorage(exporter).
        WithMemoryStorage().
        Build()
}
```

## Migration Guide

### From Manual Storage

If you were using:

```go
client := sdklog.NewSimpleKeyValueStorageClient()
adapter, _ := sdklog.NewAuditLogStorageExtensionAdapter(client)
processor, _ := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).Build()
```

Change to:

```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithMemoryStorage().
    Build()
```

### From Extension API

If you were using:

```go
extension, _ := sdklog.NewStorageExtension(...)
extension.Start(ctx)
client, _ := extension.GetClient(ctx, "name")
adapter, _ := sdklog.NewAuditLogStorageExtensionAdapter(client)
processor, _ := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).Build()
defer extension.Shutdown(ctx)
```

Change to:

```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(...).
    Build()
```

## Best Practices

### 1. Always Use defer Shutdown

```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(...).
    Build()
defer processor.Shutdown(ctx)  // ← Cleans up storage automatically
```

### 2. Handle Connection Errors

```go
processor, err := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(...).
    Build()
    
if err != nil {
    // Fallback or retry logic
}
```

### 3. Use Appropriate Storage for Environment

```go
var processor *sdklog.AuditLogProcessor

if production {
    processor, _ = builder.WithRedisStorage(...).Build()
} else {
    processor, _ = builder.WithMemoryStorage().Build()
}
```

## Testing

Run the example:

```bash
# Start Redis (for Redis example)
docker run -d -p 6379:6379 redis:latest

# Run examples
cd sdk/log
go run example_simple_storage.go
```

Expected output:
```
=== Simple Storage API Examples ===

Example 1: Memory Storage (simplest)
=====================================
   [EXPORTED] INFO: Example audit log #1
✅ Memory storage - no configuration needed!
   Use case: Testing, development

Example 2: Redis Storage
=========================
   [EXPORTED] INFO: Example audit log #2
✅ Redis storage configured!
   - Endpoint: localhost:6379
   - Prefix: myapp_
   - Expiration: 24h
   Use case: Distributed systems, high availability

Example 3: File Storage
=======================
   [EXPORTED] INFO: Example audit log #3
✅ File storage configured!
   - Directory: ./example_storage
   Use case: Single-node production, persistence

Example 4: SQL Storage
======================
   [EXPORTED] INFO: Example audit log #4
✅ SQL storage configured!
   - Driver: sqlite3
   - Database: :memory:
   Use case: Enterprise, compliance, SQL queries
```

## FAQ

### Q: Do I need to manage the storage extension myself?

**A:** No! The processor handles it automatically. Just call `processor.Shutdown(ctx)`.

### Q: Can I still use the old API?

**A:** Yes! `NewAuditLogProcessorBuilder(exporter, store)` still works for backward compatibility.

### Q: How do I switch storage types?

**A:** Just change the `.With*Storage()` call. Everything else stays the same.

### Q: What if Redis/SQL is unavailable?

**A:** Build() returns an error. Implement fallback logic as needed.

### Q: Do I need to call extension.Shutdown()?

**A:** No! Processor shutdown automatically closes the extension.

## See Also

- [Quick Start](QUICK_START_SIMPLE_API.md) - 30-second quickstart
- [Storage Examples](STORAGE_EXAMPLES.md) - Stress test CLI examples
- [example_simple_storage.go](../../../sdk/log/example_simple_storage.go) - Runnable code examples
- [START_HERE.md](START_HERE.md) - Documentation overview

