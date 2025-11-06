# Quick Start - Simple Storage API

## 30-Second Quickstart

### Memory Storage
```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithMemoryStorage().
    Build()
defer processor.Shutdown(ctx)
```

### Redis Storage
```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithRedisStorage(
        sdklog.WithRedisEndpoint("localhost:6379"),
    ).
    Build()
defer processor.Shutdown(ctx)
```

### File Storage
```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithFileStorage("./storage").
    Build()
defer processor.Shutdown(ctx)
```

### SQL Storage
```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithSQLStorage(
        sdklog.WithSQLDriver("postgres"),
        sdklog.WithSQLDatasource("postgresql://..."),
    ).
    Build()
defer processor.Shutdown(ctx)
```

## Complete Example

```go
package main

import (
    "context"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
    ctx := context.Background()
    
    // Create exporter
    exporter, _ := sdklog.NewOTLPExporter("http://localhost:4318")
    
    // Create processor with storage - ONE LINE!
    processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
        WithRedisStorage(
            sdklog.WithRedisEndpoint("localhost:6379"),
        ).
        Build()
    defer processor.Shutdown(ctx)
    
    // Use it
    record := &sdklog.Record{}
    // ... configure record
    processor.OnEmit(ctx, record)
    processor.ForceFlush(ctx)
}
```

## All Options

### Memory
```go
.WithMemoryStorage()  // No options
```

### Redis
```go
.WithRedisStorage(
    sdklog.WithRedisEndpoint("host:port"),           // Required
    sdklog.WithRedisAuth("password", 0),             // Optional
    sdklog.WithRedisKeyPrefix("myapp_"),            // Optional
    sdklog.WithRedisKeyExpiration(24*time.Hour),    // Optional
)
```

### File
```go
.WithFileStorage("/path/to/directory")
```

### SQL
```go
.WithSQLStorage(
    sdklog.WithSQLDriver("postgres"),           // Required
    sdklog.WithSQLDatasource("postgresql://..."),  // Required
    sdklog.WithSQLTable("audit_logs"),         // Optional
)
```

## Run Example

```bash
cd sdk/log
go run example_simple_storage.go
```

## Full Documentation

- [USER_GUIDE_SIMPLE_STORAGE.md](USER_GUIDE_SIMPLE_STORAGE.md) - Complete guide
- [FINAL_SUMMARY.md](FINAL_SUMMARY.md) - Overview

That's it! 🎉

