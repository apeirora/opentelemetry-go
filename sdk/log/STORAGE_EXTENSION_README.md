# Storage Extension Integration for Audit Log Processor

This document describes how to use OpenTelemetry Collector storage extensions with the Audit Log Processor in the OpenTelemetry Go SDK.

## Overview

The Audit Log Processor now supports pluggable storage backends through a `StorageClient` interface, compatible with the OpenTelemetry Collector's storage extension pattern. This allows users to choose any storage method that fits their needs, including:

- **In-Memory Storage** - Fast, ephemeral storage for development/testing
- **File Storage** - Persistent local file system storage
- **Database Storage** - SQL databases (SQLite, PostgreSQL, MySQL, etc.)
- **Redis Storage** - Distributed key-value storage
- **Custom Storage** - Any storage backend implementing the `StorageClient` interface

## StorageClient Interface

The `StorageClient` interface provides a simple key-value storage abstraction:

```go
type StorageClient interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte) error
    Delete(ctx context.Context, key string) error
    Batch(ctx context.Context, ops ...Operation) error
    Close(ctx context.Context) error
}
```

## Built-in Storage Clients

### 1. SimpleKeyValueStorageClient (In-Memory)

Fast in-memory storage for development and testing. Data is lost when the application stops.

```go
client := log.NewSimpleKeyValueStorageClient()
adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

**Use Cases:**
- Development and testing
- High-performance scenarios where persistence is not required
- Temporary audit logging

### 2. BoltDBStorageClient (File-Based)

Persistent file-based storage using a key-value database approach.

```go
client, err := log.NewBoltDBStorageClient("/var/log/audit/storage.db")
if err != nil {
    panic(err)
}

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

**Use Cases:**
- Single-instance applications
- Local persistent storage
- No external dependencies required

### 3. RedisStorageClient (Distributed)

Redis-based storage for distributed systems and high-performance scenarios.

```go
config := log.RedisStorageConfig{
    Endpoint:   "localhost:6379",
    Password:   "your-password",
    DB:         0,
    Prefix:     "audit_",
    Expiration: 24 * time.Hour,
}

client, err := log.NewRedisStorageClient(config)
if err != nil {
    panic(err)
}

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

**Configuration Options:**
- `Endpoint` - Redis server address (e.g., "localhost:6379")
- `Password` - Redis authentication password (optional)
- `DB` - Redis database index (default: 0)
- `Prefix` - Key prefix for all audit log entries (optional)
- `Expiration` - TTL for stored keys (optional)

**Use Cases:**
- Distributed applications
- High-availability requirements
- Shared state across multiple instances
- Automatic expiration of old audit logs

### 4. SQLStorageClient (Database)

SQL database storage supporting various database engines.

```go
config := log.SQLStorageConfig{
    Driver:     "sqlite3",
    Datasource: "file:audit.db?cache=shared&mode=rwc",
    TableName:  "audit_logs",
}

client, err := log.NewSQLStorageClient(config)
if err != nil {
    panic(err)
}

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetScheduleDelay(1 * time.Second).
    SetMaxExportBatchSize(100).
    Build()
```

**Supported Databases:**
- SQLite (`sqlite3`)
- PostgreSQL (`pgx`, `postgres`)
- MySQL (`mysql`)
- SQL Server (`sqlserver`)

**Use Cases:**
- Enterprise applications with existing database infrastructure
- Complex querying requirements
- ACID compliance requirements
- Long-term audit log retention

## Custom Storage Implementation

You can implement your own storage backend by implementing the `StorageClient` interface:

```go
type MyCustomStorageClient struct {
    // Your custom fields
}

func (c *MyCustomStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
    // Implement retrieval logic
}

func (c *MyCustomStorageClient) Set(ctx context.Context, key string, value []byte) error {
    // Implement storage logic
}

func (c *MyCustomStorageClient) Delete(ctx context.Context, key string) error {
    // Implement deletion logic
}

func (c *MyCustomStorageClient) Batch(ctx context.Context, ops ...Operation) error {
    // Implement batch operations
}

func (c *MyCustomStorageClient) Close(ctx context.Context) error {
    // Implement cleanup logic
}
```

### Example: MongoDB Storage Client

```go
type MongoDBStorageClient struct {
    client     *mongo.Client
    database   string
    collection string
}

func NewMongoDBStorageClient(uri, database, collection string) (*MongoDBStorageClient, error) {
    client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
    if err != nil {
        return nil, err
    }
    
    return &MongoDBStorageClient{
        client:     client,
        database:   database,
        collection: collection,
    }, nil
}

func (c *MongoDBStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
    coll := c.client.Database(c.database).Collection(c.collection)
    
    var result struct {
        Key   string `bson:"_id"`
        Value []byte `bson:"value"`
    }
    
    err := coll.FindOne(ctx, bson.M{"_id": key}).Decode(&result)
    if err != nil {
        return nil, err
    }
    
    return result.Value, nil
}

func (c *MongoDBStorageClient) Set(ctx context.Context, key string, value []byte) error {
    coll := c.client.Database(c.database).Collection(c.collection)
    
    _, err := coll.UpdateOne(
        ctx,
        bson.M{"_id": key},
        bson.M{"$set": bson.M{"value": value}},
        options.Update().SetUpsert(true),
    )
    
    return err
}

// Implement Delete, Batch, and Close methods...
```

## Integration with OpenTelemetry Collector Storage Extensions

If you're using the OpenTelemetry Collector, you can integrate with its storage extensions:

### File Storage Extension

```yaml
extensions:
  file_storage:
    directory: /var/lib/otelcol/audit_storage
    timeout: 1s
    create_directory: true
    compaction:
      on_start: true
```

### Database Storage Extension

```yaml
extensions:
  db_storage:
    driver: "postgres"
    datasource: "postgresql://user:password@localhost:5432/auditdb?sslmode=disable"
```

### Redis Storage Extension

```yaml
extensions:
  redis_storage:
    endpoint: redis:6379
    password: ${REDIS_PASSWORD}
    db: 0
    prefix: otel_audit_
    expiration: 168h  # 7 days
```

## Migration from AuditLogFileStore

If you're currently using `AuditLogFileStore`, you can easily migrate to the storage extension pattern:

### Before (AuditLogFileStore)

```go
store, err := log.NewAuditLogFileStore("/var/log/audit")
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, store).Build()
```

### After (Storage Extension)

```go
client, err := log.NewBoltDBStorageClient("/var/log/audit/storage.db")
if err != nil {
    panic(err)
}

adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    panic(err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).Build()
```

## Performance Considerations

### In-Memory Storage
- **Pros:** Fastest performance, no I/O overhead
- **Cons:** No persistence, limited by available memory
- **Best for:** Development, testing, non-critical audit logs

### File-Based Storage
- **Pros:** Persistent, no external dependencies, good performance
- **Cons:** Single-node only, file system I/O overhead
- **Best for:** Single-instance applications, local persistence

### Redis Storage
- **Pros:** Distributed, high performance, automatic expiration
- **Cons:** Requires Redis infrastructure, network overhead
- **Best for:** Distributed systems, high-availability scenarios

### SQL Storage
- **Pros:** ACID compliance, complex querying, enterprise-ready
- **Cons:** Slower than key-value stores, requires database infrastructure
- **Best for:** Enterprise applications, long-term retention, compliance

## Benchmarks

Typical performance characteristics (operations per second):

| Storage Client | Write Ops/sec | Read Ops/sec | Notes |
|----------------|---------------|--------------|-------|
| SimpleKeyValue | ~500,000 | ~1,000,000 | In-memory, no persistence |
| BoltDB | ~10,000 | ~50,000 | File-based, durable writes |
| Redis | ~50,000 | ~100,000 | Network overhead, distributed |
| SQL (SQLite) | ~5,000 | ~20,000 | ACID compliance, file-based |
| SQL (PostgreSQL) | ~8,000 | ~30,000 | Network overhead, ACID |

*Benchmarks are approximate and depend on hardware, configuration, and workload.*

## Error Handling

All storage clients should handle errors gracefully:

```go
client := log.NewSimpleKeyValueStorageClient()
adapter, err := log.NewAuditLogStorageExtensionAdapter(client)
if err != nil {
    log.Fatalf("Failed to create storage adapter: %v", err)
}

processor, err := log.NewAuditLogProcessorBuilder(exporter, adapter).
    SetExceptionHandler(&log.CustomAuditExceptionHandler{
        OnException: func(exception *log.AuditException) {
            // Handle storage errors
            log.Printf("Audit exception: %v", exception)
        },
    }).
    Build()
```

## Best Practices

1. **Choose the Right Storage Backend**
   - Use in-memory for development/testing
   - Use file-based for single-node production
   - Use Redis for distributed systems
   - Use SQL for enterprise/compliance requirements

2. **Configure Appropriate Batch Sizes**
   - Larger batches reduce storage operations
   - Smaller batches reduce memory usage
   - Balance based on your workload

3. **Set Appropriate Timeouts**
   - Storage operations should have reasonable timeouts
   - Consider network latency for distributed storage

4. **Monitor Storage Performance**
   - Track write latency
   - Monitor storage capacity
   - Set up alerts for storage errors

5. **Plan for Disaster Recovery**
   - Regular backups for file-based storage
   - Replication for distributed storage
   - Test recovery procedures

## Troubleshooting

### Storage Client Connection Failures

```go
// Add retry logic for storage client initialization
var client log.StorageClient
var err error

for attempts := 0; attempts < 3; attempts++ {
    client, err = log.NewRedisStorageClient(config)
    if err == nil {
        break
    }
    time.Sleep(time.Second * time.Duration(attempts+1))
}

if err != nil {
    log.Fatalf("Failed to create storage client after retries: %v", err)
}
```

### High Storage Latency

- Check network connectivity for distributed storage
- Verify storage backend is not overloaded
- Consider increasing batch sizes
- Review storage backend configuration

### Storage Capacity Issues

- Implement automatic cleanup of old audit logs
- Use Redis expiration for automatic cleanup
- Implement log rotation for file-based storage
- Monitor storage usage with alerts

## Examples

See the following files for complete examples:
- `example_storage_extension.go` - Basic usage examples
- `audit_storage_extension_test.go` - Test examples and patterns

## See Also

- [OpenTelemetry Collector Storage Extension](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/storage)
- [Audit Log Processor README](AUDIT_LOG_README.md)
- [Design Document](DESIGN.md)

