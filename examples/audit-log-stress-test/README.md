# Audit Log Stress Test

This example demonstrates stress testing the OpenTelemetry Audit Log processor by sending a large volume of logs.

## Features

- Send 1M+ audit logs with unique identifiers
- Each log contains:
  - **Test Run UUID**: Same UUID for all logs in a test run (for easy filtering)
  - **Unique Counter**: Sequential counter (1 to N) to verify all logs are received
  - Batch information and metadata
- Real-time progress reporting
- Performance metrics (logs/second)
- **Multiple storage backends**: In-memory, File, Redis, SQL databases
- Compatible with OpenTelemetry Collector storage extensions
- Verification instructions

## Prerequisites

1. **OTEL Collector** running on `http://localhost:4318`
   
   Quick start with Docker:
   ```bash
   docker run -p 4318:4318 otel/opentelemetry-collector:latest
   ```

2. **Go 1.21+** installed

## Running the Stress Test

### Default Test (1 Million Logs)

```bash
go run .
```

### Quick Test (10k Logs)

```bash
go run . -quick
```

### Mega Test (5 Million Logs)

```bash
go run . -mega
```

### Custom Configuration

```bash
go run . \
  -logs 2000000 \
  -batch 20000 \
  -report 200000 \
  -endpoint http://localhost:4318 \
  -delay 50ms \
  -export-batch 2000
```

### Storage Backend Examples

#### In-Memory Storage (Default)

```bash
go run . -storage memory
```

#### File-Based Storage

```bash
go run . -storage file -storage-path /var/log/stress_test.db
```

#### Redis Storage

```bash
go run . \
  -storage redis \
  -redis-endpoint localhost:6379 \
  -redis-password mypassword \
  -redis-db 1
```

#### SQL Database Storage

**SQLite:**
```bash
go run . \
  -storage sql \
  -sql-driver sqlite3 \
  -sql-datasource "file:stress_test.db?cache=shared"
```

**PostgreSQL:**
```bash
go run . \
  -storage sql \
  -sql-driver postgres \
  -sql-datasource "postgresql://user:pass@localhost:5432/auditdb?sslmode=disable"
```

**MySQL:**
```bash
go run . \
  -storage sql \
  -sql-driver mysql \
  -sql-datasource "user:pass@tcp(localhost:3306)/auditdb"
```

## Command Line Options

### Core Options

| Flag | Default | Description |
|------|---------|-------------|
| `-logs` | 1,000,000 | Total number of logs to send |
| `-batch` | 10,000 | Batch size for logical grouping |
| `-report` | 100,000 | Progress report interval |
| `-endpoint` | http://localhost:4318 | OTLP endpoint URL |
| `-delay` | 100ms | Schedule delay between exports |
| `-export-batch` | 1000 | Maximum export batch size |
| `-uuid` | auto | Custom test run UUID |
| `-quick` | false | Run quick test (10k logs) |
| `-mega` | false | Run mega test (5M logs) |

### Storage Options

| Flag | Default | Description |
|------|---------|-------------|
| `-storage` | memory | Storage type: `memory`, `file`, `redis`, `sql` |
| `-storage-path` | ./stress_test_storage.db | File path for file-based storage |
| `-redis-endpoint` | localhost:6379 | Redis server endpoint |
| `-redis-password` | "" | Redis authentication password |
| `-redis-db` | 0 | Redis database number (0-15) |
| `-sql-driver` | sqlite3 | SQL driver: `sqlite3`, `postgres`, `mysql` |
| `-sql-datasource` | :memory: | SQL connection string |

## Example Output

```
=== Audit Log Stress Test ===

Configuration:
  Total Logs:        1000000
  Batch Size:        10000
  Report Interval:   100000
  Test Run UUID:     1728582400-1234-5678-9012-123456789012
  OTLP Endpoint:     http://localhost:4318
  Schedule Delay:    100ms
  Max Export Batch:  1000

Step 1: Creating OTLP Exporter
✅ OTLP Exporter created

Step 2: Creating Audit Log Store
✅ memory store created

Step 3: Creating Audit Processor
✅ Audit processor created

Step 4: Starting stress test - emitting logs
────────────────────────────────────────────
📊 Progress: 100000/1000000 logs (10.0%) | 50000 logs/sec | Queue: 5000 | Errors: 0
📊 Progress: 200000/1000000 logs (20.0%) | 55000 logs/sec | Queue: 3000 | Errors: 0
...
────────────────────────────────────────────
✅ Emission completed in 18.5s
   Success: 1000000
   Errors:  0
   Rate:    54054 logs/sec
   Queue:   2450 pending

Step 5: Flushing remaining logs to OTEL Collector
✅ All logs flushed in 3.2s

Step 6: Test Summary
════════════════════════════════════════════
🎯 Test Run UUID:     1728582400-1234-5678-9012-123456789012
📝 Total Logs Sent:   1000000
❌ Failed Logs:       0
⏱️  Total Time:        21.7s
📊 Average Rate:      46083 logs/sec
🔢 Counter Range:     1 to 1000000
════════════════════════════════════════════

🔍 Verification Instructions:
   1. Query your observability backend for logs with:
      test.run.uuid = "1728582400-1234-5678-9012-123456789012"
   2. Count total logs received
   3. Verify test.log.counter ranges from 1 to 1000000
   4. Check for any gaps in the counter sequence
```

## Log Attributes

Each stress test log contains these attributes:

| Attribute | Type | Description |
|-----------|------|-------------|
| `test.run.uuid` | string | UUID for this test run (same for all logs) |
| `test.log.counter` | int64 | Unique sequential counter (1 to N) |
| `test.type` | string | Always "stress_test" |
| `test.batch` | int64 | Logical batch number |
| `test.batch.position` | int64 | Position within the batch |

## Verifying Results

### Using OpenSearch/Elasticsearch

```json
GET /logs/_search
{
  "query": {
    "term": { "test.run.uuid": "YOUR-UUID-HERE" }
  },
  "size": 0,
  "aggs": {
    "total_count": { "value_count": { "field": "test.log.counter" } },
    "min_counter": { "min": { "field": "test.log.counter" } },
    "max_counter": { "max": { "field": "test.log.counter" } }
  }
}
```

### Using PromQL (if exported to Prometheus)

```promql
count(log_records{test_run_uuid="YOUR-UUID-HERE"})
```

### Using SQL (if exported to database)

```sql
SELECT 
    COUNT(*) as total_logs,
    MIN(counter) as min_counter,
    MAX(counter) as max_counter,
    MAX(counter) - MIN(counter) + 1 - COUNT(*) as missing_logs
FROM logs
WHERE test_run_uuid = 'YOUR-UUID-HERE';
```

## Performance Tips

1. **Increase export batch size** for better throughput:
   ```bash
   go run . -export-batch 5000
   ```

2. **Decrease schedule delay** for faster exports:
   ```bash
   go run . -delay 50ms
   ```

3. **Choose appropriate storage backend**:
   - **In-Memory**: Fastest, but no persistence
   - **File**: Good balance of speed and persistence
   - **Redis**: Best for distributed systems
   - **SQL**: Best for enterprise/compliance needs

4. **Monitor OTEL Collector** resources during the test

5. **Run multiple tests** with different configurations to find optimal settings

## Storage Backend Comparison

| Storage | Performance | Persistence | Distributed | Best For |
|---------|-------------|-------------|-------------|----------|
| In-Memory | ⚡⚡⚡⚡⚡ | ❌ | ❌ | Development, testing |
| File | ⚡⚡⚡⚡ | ✅ | ❌ | Single-node production |
| Redis | ⚡⚡⚡⚡ | ✅ | ✅ | Distributed systems |
| SQL | ⚡⚡⚡ | ✅ | ✅ | Enterprise, compliance |

## Troubleshooting

### Connection Refused

If you see connection errors, ensure OTEL Collector is running:
```bash
docker ps | grep otel
```

### Slow Performance

- Check OTEL Collector configuration
- Increase `-export-batch` size
- Decrease `-delay` duration
- Monitor system resources

### Missing Logs

- Check OTEL Collector logs for errors
- Verify backend storage capacity
- Increase timeout values if needed

## Next Steps

After running the stress test:

1. Query your observability backend using the Test Run UUID
2. Verify all logs were received (counter from 1 to N)
3. Check for any performance bottlenecks
4. Adjust configuration for your specific use case

## Additional Resources

- **[Storage Examples](docs/STORAGE_EXAMPLES.md)** - Detailed examples for each storage backend
- **[Storage Quickstart](docs/STORAGE_QUICKSTART.md)** - Quick start guide for storage backends
- **[Storage Integration Summary](docs/STORAGE_INTEGRATION_SUMMARY.md)** - Storage integration overview
- **[Storage Migration Guide](docs/STORAGE_MIGRATION_GUIDE.md)** - Guide for migrating storage implementations
- **[Audit Log Summary](docs/AUDIT_LOG_SUMMARY.md)** - Audit log feature summary
- **[Audit Log Comparison](docs/AUDIT_LOG_COMPARISON.md)** - Comparison with other audit log solutions
- **[Storage Extension README](../../sdk/log/STORAGE_EXTENSION_README.md)** - Complete storage extension documentation
- **[Audit Log README](../../sdk/log/AUDIT_LOG_README.md)** - Audit processor documentation

