# Storage Extensions - Documentation

Welcome! This is your complete guide to using storage extensions with the OpenTelemetry Go SDK audit log processor.

## 🚀 Quick Start (30 seconds)

### Memory Storage (Simplest)
```go
processor, _ := sdklog.NewAuditLogProcessorWithStorage(exporter).
    WithMemoryStorage().
    Build()
defer processor.Shutdown(ctx)
```

### Redis Storage (Production)
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

## 📚 Full Documentation

- **[QUICK_START_SIMPLE_API.md](QUICK_START_SIMPLE_API.md)** - More examples and all options
- **[USER_GUIDE_SIMPLE_STORAGE.md](USER_GUIDE_SIMPLE_STORAGE.md)** - Complete guide with migration info
- **[STORAGE_EXAMPLES.md](STORAGE_EXAMPLES.md)** - Stress test CLI examples

## 🧪 Run the Stress Test

```bash
# Start Redis
docker run -d -p 6379:6379 redis:latest

# Run stress test
cd examples/audit-log-stress-test
go run . -quick -storage redis
```

See [STORAGE_EXAMPLES.md](STORAGE_EXAMPLES.md) for more CLI examples.

## 📖 Other Docs

- **[AUDIT_LOG_SUMMARY.md](AUDIT_LOG_SUMMARY.md)** - Audit log system overview
- **[README.md](README.md)** - Documentation directory overview
- **[SDK Audit Log API](../../../sdk/log/AUDIT_LOG_README.md)** - API documentation

---

**Need help? Start with [USER_GUIDE_SIMPLE_STORAGE.md](USER_GUIDE_SIMPLE_STORAGE.md)** 📖

