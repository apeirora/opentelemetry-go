# Storage Extension Quick Start Guide

Get started with OpenTelemetry Collector-compatible storage extensions in 5 minutes.

## Step 1: Choose Your Storage Backend

| Storage | When to Use | Setup Time |
|---------|-------------|------------|
| **In-Memory** | Development, testing | 0 min |
| **File** | Single-node production | 0 min |
| **Redis** | Distributed systems | 2 min |
| **SQL** | Enterprise/compliance | 2-5 min |

## Step 2: Quick Setup Examples

### Option A: In-Memory (Fastest Start)

```go
package main

import (
    "context"
    "time"
    "go.opentelemetry.io/otel/sdk/log"
)

func main() {
    client := log.NewSimpleKeyValueStorageClient()
    adapter, _ := log.NewAuditLogStorageExtensionAdapter(client)
    
    exporter := yourExporter // Your OTLP or custom exporter
    
    processor, _ := log.NewAuditLogProcessorBuilder(exporter, adapter).
        SetScheduleDelay(1 * time.Second).
        SetMaxExportBatchSize(100).
        Build()
    defer processor.Shutdown(context.Background())
    
    // Use processor...
}
```

### Option B: File Storage (Persistent)

```go
client, _ := log.NewBoltDBStorageClient("./audit_logs.db")
adapter, _ := log.NewAuditLogStorageExtensionAdapter(client)

processor, _ := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    Build()
```

### Option C: Redis (Distributed)

**1. Start Redis:**
```bash
docker run -d -p 6379:6379 redis:latest
```

**2. Use Redis storage:**
```go
config := log.RedisStorageConfig{
    Endpoint:   "localhost:6379",
    Password:   "",
    DB:         0,
    Prefix:     "audit_",
    Expiration: 24 * time.Hour,
}

client, _ := log.NewRedisStorageClient(config)
adapter, _ := log.NewAuditLogStorageExtensionAdapter(client)

processor, _ := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    Build()
```

### Option D: SQL Database (Enterprise)

**1. Start PostgreSQL:**
```bash
docker run -d \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=auditdb \
  -p 5432:5432 \
  postgres:latest
```

**2. Use SQL storage:**
```go
config := log.SQLStorageConfig{
    Driver:     "postgres",
    Datasource: "postgresql://postgres:password@localhost:5432/auditdb?sslmode=disable",
    TableName:  "audit_logs",
}

client, _ := log.NewSQLStorageClient(config)
adapter, _ := log.NewAuditLogStorageExtensionAdapter(client)

processor, _ := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    Build()
```

## Step 3: Emit Audit Logs

```go
record := &log.Record{}
record.SetTimestamp(time.Now())
record.SetObservedTimestamp(time.Now())
record.SetSeverity(log.SeverityInfo)
record.SetSeverityText("INFO")
record.SetBody(log.StringValue("User logged in"))

record.AddAttributes(
    log.String("user_id", "12345"),
    log.String("action", "login"),
)

err := processor.OnEmit(context.Background(), record)
if err != nil {
    log.Printf("Failed to emit: %v", err)
}
```

## Step 4: Test with Stress Test

### Quick Test (10k logs)
```bash
cd examples/audit-log-stress-test

# In-memory (default)
go run . -quick

# File storage
go run . -quick -storage file

# Redis
go run . -quick -storage redis

# SQL
go run . -quick -storage sql
```

### Full Test (1M logs)
```bash
# Default configuration
go run .

# With Redis
go run . -storage redis -redis-endpoint localhost:6379

# With PostgreSQL
go run . \
  -storage sql \
  -sql-driver postgres \
  -sql-datasource "postgresql://postgres:password@localhost:5432/auditdb?sslmode=disable"
```

## Step 5: Complete Example

Here's a complete working example:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "go.opentelemetry.io/otel/log"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
    ctx := context.Background()
    
    // 1. Create storage client
    client := sdklog.NewSimpleKeyValueStorageClient()
    adapter, err := sdklog.NewAuditLogStorageExtensionAdapter(client)
    if err != nil {
        panic(err)
    }
    
    // 2. Create exporter (console for demo)
    exporter := &ConsoleExporter{}
    
    // 3. Create processor
    processor, err := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
        SetScheduleDelay(1 * time.Second).
        SetMaxExportBatchSize(100).
        SetExporterTimeout(30 * time.Second).
        Build()
    if err != nil {
        panic(err)
    }
    defer processor.Shutdown(ctx)
    
    // 4. Emit logs
    for i := 0; i < 10; i++ {
        record := &sdklog.Record{}
        record.SetTimestamp(time.Now())
        record.SetObservedTimestamp(time.Now())
        record.SetSeverity(log.SeverityInfo)
        record.SetSeverityText("INFO")
        record.SetBody(log.StringValue(fmt.Sprintf("Audit event #%d", i)))
        
        if err := processor.OnEmit(ctx, record); err != nil {
            fmt.Printf("Failed to emit: %v\n", err)
        }
    }
    
    // 5. Flush
    processor.ForceFlush(ctx)
    fmt.Println("Done!")
}

type ConsoleExporter struct{}

func (e *ConsoleExporter) Export(ctx context.Context, records []sdklog.Record) error {
    for _, r := range records {
        fmt.Printf("[%s] %s: %s\n", r.Severity(), r.Timestamp().Format(time.RFC3339), r.Body())
    }
    return nil
}

func (e *ConsoleExporter) Shutdown(ctx context.Context) error { return nil }
func (e *ConsoleExporter) ForceFlush(ctx context.Context) error { return nil }
```

## Common Patterns

### Pattern 1: Configuration from Environment

```go
import "os"

func createStorage() (sdklog.AuditLogStore, error) {
    storageType := os.Getenv("AUDIT_STORAGE_TYPE") // memory, file, redis, sql
    
    switch storageType {
    case "redis":
        config := sdklog.RedisStorageConfig{
            Endpoint:   os.Getenv("REDIS_ENDPOINT"),
            Password:   os.Getenv("REDIS_PASSWORD"),
            DB:         0,
            Prefix:     "audit_",
            Expiration: 24 * time.Hour,
        }
        client, err := sdklog.NewRedisStorageClient(config)
        if err != nil {
            return nil, err
        }
        return sdklog.NewAuditLogStorageExtensionAdapter(client)
        
    case "file":
        path := os.Getenv("AUDIT_STORAGE_PATH")
        if path == "" {
            path = "./audit_logs.db"
        }
        client, err := sdklog.NewBoltDBStorageClient(path)
        if err != nil {
            return nil, err
        }
        return sdklog.NewAuditLogStorageExtensionAdapter(client)
        
    default: // memory
        client := sdklog.NewSimpleKeyValueStorageClient()
        return sdklog.NewAuditLogStorageExtensionAdapter(client)
    }
}
```

### Pattern 2: Graceful Shutdown

```go
import "os/signal"

func main() {
    ctx := context.Background()
    
    // Create processor...
    processor, _ := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).Build()
    
    // Handle shutdown gracefully
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        fmt.Println("\nShutting down gracefully...")
        
        // Flush remaining logs
        if err := processor.ForceFlush(ctx); err != nil {
            fmt.Printf("Flush error: %v\n", err)
        }
        
        // Shutdown
        if err := processor.Shutdown(ctx); err != nil {
            fmt.Printf("Shutdown error: %v\n", err)
        }
        
        os.Exit(0)
    }()
    
    // Your application logic...
}
```

### Pattern 3: Custom Error Handling

```go
type CustomHandler struct{}

func (h *CustomHandler) Handle(exception *sdklog.AuditException) {
    // Log to monitoring system
    fmt.Printf("ALERT: Audit failure: %s\n", exception.Message)
    
    // Send to error tracking
    // sendToSentry(exception)
    
    // Trigger alert
    // triggerPagerDuty(exception)
}

// Use custom handler
processor, _ := sdklog.NewAuditLogProcessorBuilder(exporter, adapter).
    SetExceptionHandler(&CustomHandler{}).
    Build()
```

## Troubleshooting

### Issue: "Failed to create storage"

**Solution:** Check storage backend is accessible:

```bash
# Redis
redis-cli ping

# PostgreSQL
psql -h localhost -U postgres -c "SELECT 1"

# File permissions
ls -la ./audit_logs.db
```

### Issue: "High queue size"

**Solution:** Adjust batch size and delay:

```go
processor, _ := NewAuditLogProcessorBuilder(exporter, adapter).
    SetMaxExportBatchSize(2000).  // Increase batch size
    SetScheduleDelay(50 * time.Millisecond).  // Decrease delay
    Build()
```

### Issue: "Slow performance"

**Solution:** Choose faster storage or tune configuration:

1. Use in-memory for maximum speed
2. Use Redis for distributed + speed
3. Increase batch sizes
4. Decrease schedule delay

## Next Steps

1. ✅ **You now have storage working!**
2. 📖 **Read detailed docs:**
   - [Storage Extension README](STORAGE_EXTENSION_README.md)
   - [Migration Guide](STORAGE_MIGRATION_GUIDE.md)
3. 🧪 **Run stress test** to verify performance
4. 🚀 **Deploy to production** with your chosen storage
5. 📊 **Monitor** queue sizes and performance

## Need Help?

- **Examples:** See `example_storage_extension.go` and `example_collector_integration.go`
- **Stress Test:** See `examples/audit-log-stress-test/`
- **Full Docs:** See `STORAGE_EXTENSION_README.md`
- **Migration:** See `STORAGE_MIGRATION_GUIDE.md`

---

**You're ready to go! Start with in-memory storage and scale up as needed.** 🎉

