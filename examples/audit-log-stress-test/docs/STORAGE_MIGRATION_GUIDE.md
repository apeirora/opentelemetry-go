# Migration Guide: From AuditLogFileStore to Storage Extensions

This guide helps you migrate from the original `AuditLogFileStore` and `AuditLogInMemoryStore` implementations to the new Storage Extension pattern, which provides compatibility with OpenTelemetry Collector storage backends.

## Why Migrate?

The new Storage Extension pattern provides:

- **Flexibility**: Choose from multiple storage backends (file, Redis, SQL, custom)
- **Compatibility**: Works with OpenTelemetry Collector storage extensions
- **Scalability**: Support for distributed storage with Redis
- **Enterprise Ready**: SQL database support for compliance and long-term retention
- **Extensibility**: Easy to implement custom storage backends

## Quick Migration Examples

### Example 1: File-Based Storage

**Before (Old AuditLogFileStore):**
```go
store, err := log.NewAuditLogFileStore("/var/log/audit")
if err != nil {
    log.Fatal(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

**After (Storage Extension):**
```go
// Option 1: Use BoltDB-based storage (similar to file storage)
client, err := log.NewBoltDBStorageClient("/var/log/audit/storage.db")
if err != nil {
    log.Fatal(err)
}

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    log.Fatal(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

### Example 2: In-Memory Storage

**Before (Old AuditLogInMemoryStore):**
```go
store := log.NewAuditLogInMemoryStore()

processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
    SetScheduleDelay(1 * time.Second).
    Build()
```

**After (Storage Extension):**
```go
client := log.NewSimpleKeyValueStorageClient()

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    log.Fatal(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    Build()
```

## Feature Comparison

| Feature | AuditLogFileStore | Storage Extension |
|---------|-------------------|-------------------|
| File-based persistence | ✅ | ✅ (via BoltDBStorageClient) |
| In-memory storage | ✅ (separate impl) | ✅ (via SimpleKeyValueStorageClient) |
| JSON serialization | ✅ | ✅ |
| Deduplication | ✅ | ✅ |
| Redis support | ❌ | ✅ |
| SQL database support | ❌ | ✅ |
| Custom storage backends | ❌ | ✅ |
| OTel Collector compatible | ❌ | ✅ |

## Detailed Migration Steps

### Step 1: Identify Your Current Storage

Determine which storage implementation you're currently using:

1. **AuditLogFileStore** → Migrate to `BoltDBStorageClient` or `SQLStorageClient`
2. **AuditLogInMemoryStore** → Migrate to `SimpleKeyValueStorageClient`

### Step 2: Choose Your New Storage Backend

Consider your requirements:

| Requirement | Recommended Storage |
|-------------|---------------------|
| Single-node, persistent | `BoltDBStorageClient` |
| Development/testing | `SimpleKeyValueStorageClient` |
| Distributed system | `RedisStorageClient` |
| Enterprise/compliance | `SQLStorageClient` |
| High performance | `RedisStorageClient` |
| No external dependencies | `BoltDBStorageClient` |

### Step 3: Update Your Code

Replace the old store initialization with the new storage client + adapter pattern:

```go
// Old code
store, err := log.NewAuditLogFileStore(path)

// New code
client, err := log.NewBoltDBStorageClient(path + "/storage.db")
adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
```

### Step 4: Update Processor Builder

The processor builder API remains the same, just pass the adapter instead of the store:

```go
// Both old and new use the same builder pattern
processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    SetExporterTimeout(30 * time.Second).
    Build()
```

### Step 5: Test Your Migration

Verify that your audit logs are being stored and exported correctly:

```go
// Emit a test record
record := &Record{}
record.SetTimestamp(time.Now())
record.SetObservedTimestamp(time.Now())
record.SetSeverity(log.SeverityInfo)
record.SetSeverityText("INFO")
record.SetBody(log.StringValue("Test migration"))

err := processor.OnEmit(ctx, record)
if err != nil {
    log.Printf("Migration test failed: %v", err)
}

// Force flush and verify export
err = processor.ForceFlush(ctx)
if err != nil {
    log.Printf("Flush failed: %v", err)
}
```

## Advanced Migration Scenarios

### Scenario 1: Migrating Existing Data

If you have existing audit logs in the old format, you'll need to migrate the data:

```go
func migrateData(oldStorePath, newStorePath string) error {
    // Create old store to read data
    oldStore, err := log.NewAuditLogFileStore(oldStorePath)
    if err != nil {
        return err
    }
    
    // Get all existing records
    ctx := context.Background()
    records, err := oldStore.GetAll(ctx)
    if err != nil {
        return err
    }
    
    // Create new storage
    client, err := log.NewBoltDBStorageClient(newStorePath + "/storage.db")
    if err != nil {
        return err
    }
    
    adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
    if err != nil {
        return err
    }
    
    // Migrate records
    for _, record := range records {
        if err := adapter.Save(ctx, &record); err != nil {
            log.Printf("Failed to migrate record: %v", err)
        }
    }
    
    return nil
}
```

### Scenario 2: Gradual Migration

Run both storage systems in parallel during migration:

```go
// Create both old and new storage
oldStore, _ := log.NewAuditLogFileStore("/var/log/audit")
newClient, _ := log.NewBoltDBStorageClient("/var/log/audit_new/storage.db")
newAdapter, _ := log.NewAuditLogStorageExtensionAdapter(newClient)

// Create processors for both
exporter := &YourExporter{}
oldProcessor, _ := log.NewAuditLogProcessorBuilder(exporter, oldStore).Build()
newProcessor, _ := log.NewAuditLogProcessorBuilder(exporter, newAdapter).Build()

// Emit to both during migration period
record := createRecord()
oldProcessor.OnEmit(ctx, record)
newProcessor.OnEmit(ctx, record)

// After migration period, shutdown old processor
oldProcessor.Shutdown(ctx)
// Continue with new processor only
```

### Scenario 3: Migrating to Distributed Storage

Upgrade from single-node to distributed Redis storage:

```go
// Old: Single-node file storage
oldClient, _ := log.NewBoltDBStorageClient("/var/log/audit/storage.db")
oldAdapter, _ := log.NewAuditLogStorageExtensionAdapter(oldClient)

// New: Distributed Redis storage
redisConfig := log.RedisStorageConfig{
    Endpoint:   "redis-cluster:6379",
    Password:   os.Getenv("REDIS_PASSWORD"),
    DB:         0,
    Prefix:     "audit_",
    Expiration: 7 * 24 * time.Hour, // 7 days retention
}
newClient, _ := log.NewRedisStorageClient(redisConfig)
newAdapter, _ := log.NewAuditLogStorageExtensionAdapter(newClient)

// Create processor with new distributed storage
processor, _ := log.NewAuditLogProcessorBuilder(exporter, newAdapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(500). // Larger batch for distributed system
    Build()
```

## Common Issues and Solutions

### Issue 1: File Path Changes

**Problem**: Old AuditLogFileStore used a directory path, new storage uses a database file path.

**Solution**:
```go
// Old: Directory path
oldPath := "/var/log/audit"  // Would create /var/log/audit/audit.log

// New: Database file path
newPath := "/var/log/audit/storage.db"  // Explicit database file
```

### Issue 2: Different Serialization Format

**Problem**: Old store used JSON lines format, new store uses binary key-value format.

**Solution**: Use the migration script from "Scenario 1: Migrating Existing Data" above.

### Issue 3: Performance Differences

**Problem**: Different storage backends have different performance characteristics.

**Solution**: Adjust batch sizes and delays based on your storage backend:

```go
// For file-based storage (slower)
processor, _ := NewAuditLogProcessorBuilder(exporter, fileAdapter).
    SetScheduleDelay(2 * time.Second).
    SetMaxExportBatchSize(100).
    Build()

// For Redis (faster)
processor, _ := NewAuditLogProcessorBuilder(exporter, redisAdapter).
    SetScheduleDelay(500 * time.Millisecond).
    SetMaxExportBatchSize(500).
    Build()
```

## Testing Your Migration

Create a test to verify the migration works correctly:

```go
func TestMigration(t *testing.T) {
    ctx := context.Background()
    
    // Create new storage
    client := NewSimpleKeyValueStorageClient()
    adapter, err := NewAuditLogStorageExtensionAdapter(client)
    if err != nil {
        t.Fatal(err)
    }
    
    // Create processor
    exporter := &TestExporter{}
    processor, err := NewAuditLogProcessorBuilder(exporter, adapter).
        SetScheduleDelay(100 * time.Millisecond).
        Build()
    if err != nil {
        t.Fatal(err)
    }
    defer processor.Shutdown(ctx)
    
    // Emit test records
    for i := 0; i < 10; i++ {
        record := &Record{}
        record.SetTimestamp(time.Now())
        record.SetObservedTimestamp(time.Now())
        record.SetSeverity(log.SeverityInfo)
        record.SetBody(log.StringValue(fmt.Sprintf("Test %d", i)))
        
        if err := processor.OnEmit(ctx, record); err != nil {
            t.Errorf("Failed to emit record %d: %v", i, err)
        }
    }
    
    // Force flush
    if err := processor.ForceFlush(ctx); err != nil {
        t.Errorf("Failed to flush: %v", err)
    }
    
    // Verify export
    if exporter.RecordCount() != 10 {
        t.Errorf("Expected 10 records, got %d", exporter.RecordCount())
    }
}
```

## Rollback Plan

If you need to rollback to the old implementation:

1. **Keep old code in version control**: Don't delete the old implementation immediately
2. **Run in parallel**: Keep both systems running during the migration period
3. **Monitor metrics**: Compare audit log counts between old and new systems
4. **Gradual cutover**: Gradually shift traffic from old to new storage
5. **Backup data**: Keep backups of old audit logs before migration

## Getting Help

If you encounter issues during migration:

1. Check the [Storage Extension README](STORAGE_EXTENSION_README.md) for detailed documentation
2. Review the [example files](example_storage_extension.go) for working code samples
3. Run the tests to verify your setup: `go test -v -run ".*Storage.*"`
4. Check error messages from the `AuditExceptionHandler` for storage-specific errors

## Next Steps

After successful migration:

1. **Remove old storage code**: Once verified, remove references to old storage implementations
2. **Update documentation**: Document your new storage configuration
3. **Set up monitoring**: Monitor storage performance and capacity
4. **Plan for scaling**: Consider distributed storage if you need to scale
5. **Review retention policies**: Set appropriate expiration/cleanup policies for your storage backend

## Additional Resources

- [Storage Extension README](STORAGE_EXTENSION_README.md) - Complete storage extension documentation
- [Audit Log Processor README](AUDIT_LOG_README.md) - Audit processor documentation
- [OpenTelemetry Collector Storage Extensions](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/storage) - Official OTel storage extensions

