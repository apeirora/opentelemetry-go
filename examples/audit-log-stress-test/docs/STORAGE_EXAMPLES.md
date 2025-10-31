# Storage Backend Examples for Stress Test

This document provides practical examples for using different storage backends with the Audit Log Stress Test.

## Quick Start Examples

### 1. In-Memory Storage (Default)

**Use Case:** Development, testing, maximum performance

```bash
# Default - fastest, no persistence
go run . -logs 100000 -storage memory

# Quick test with in-memory
go run . -quick
```

**Pros:**
- Fastest performance (500k+ ops/sec)
- No external dependencies
- Immediate startup

**Cons:**
- No persistence
- Lost on application restart

---

### 2. File-Based Storage

**Use Case:** Single-node production, local persistence

```bash
# Basic file storage
go run . -storage file -storage-path ./audit_storage.db

# High-volume test with file storage
go run . \
  -logs 5000000 \
  -storage file \
  -storage-path /var/log/audit/stress_test.db \
  -export-batch 2000 \
  -delay 50ms
```

**Pros:**
- Persistent across restarts
- No external dependencies
- Good performance (10k-50k ops/sec)

**Cons:**
- Single-node only
- File system I/O overhead

---

### 3. Redis Storage

**Use Case:** Distributed systems, high-availability

```bash
# Local Redis
go run . \
  -storage redis \
  -redis-endpoint localhost:6379

# Redis with authentication
go run . \
  -storage redis \
  -redis-endpoint redis.example.com:6379 \
  -redis-password "your-secure-password" \
  -redis-db 1

# High-performance distributed test
go run . \
  -logs 10000000 \
  -storage redis \
  -redis-endpoint redis-cluster:6379 \
  -export-batch 5000 \
  -delay 25ms
```

**Redis Setup:**
```bash
# Start Redis with Docker
docker run -d \
  --name redis-stress-test \
  -p 6379:6379 \
  redis:latest

# With authentication
docker run -d \
  --name redis-stress-test \
  -p 6379:6379 \
  redis:latest \
  redis-server --requirepass your-password
```

**Pros:**
- Distributed/shared storage
- High performance (50k-100k ops/sec)
- Automatic expiration support
- Multiple instances can share state

**Cons:**
- Requires Redis infrastructure
- Network latency

---

### 4. SQL Database Storage

**Use Case:** Enterprise, compliance, long-term retention

#### SQLite

```bash
# In-memory SQLite (no persistence)
go run . -storage sql

# Persistent SQLite
go run . \
  -storage sql \
  -sql-driver sqlite3 \
  -sql-datasource "file:stress_test.db?cache=shared&mode=rwc"

# WAL mode for better concurrency
go run . \
  -storage sql \
  -sql-driver sqlite3 \
  -sql-datasource "file:stress_test.db?cache=shared&_journal_mode=WAL"
```

#### PostgreSQL

```bash
# Local PostgreSQL
go run . \
  -storage sql \
  -sql-driver postgres \
  -sql-datasource "postgresql://postgres:password@localhost:5432/auditdb?sslmode=disable"

# Production PostgreSQL with SSL
go run . \
  -storage sql \
  -sql-driver postgres \
  -sql-datasource "postgresql://user:pass@db.example.com:5432/auditdb?sslmode=require"
```

**PostgreSQL Setup:**
```bash
# Start PostgreSQL with Docker
docker run -d \
  --name postgres-stress-test \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=auditdb \
  -p 5432:5432 \
  postgres:latest
```

#### MySQL

```bash
# Local MySQL
go run . \
  -storage sql \
  -sql-driver mysql \
  -sql-datasource "root:password@tcp(localhost:3306)/auditdb?parseTime=true"

# Production MySQL
go run . \
  -storage sql \
  -sql-driver mysql \
  -sql-datasource "user:pass@tcp(db.example.com:3306)/auditdb?tls=skip-verify"
```

**MySQL Setup:**
```bash
# Start MySQL with Docker
docker run -d \
  --name mysql-stress-test \
  -e MYSQL_ROOT_PASSWORD=password \
  -e MYSQL_DATABASE=auditdb \
  -p 3306:3306 \
  mysql:latest
```

**Pros:**
- ACID compliance
- Enterprise-ready
- Complex querying capabilities
- Long-term retention
- SQL Server, PostgreSQL support replication

**Cons:**
- Slower than key-value stores (5k-30k ops/sec)
- Requires database infrastructure
- More complex setup

---

## Performance Comparison

Real-world stress test results (1M logs):

| Storage Type | Total Time | Avg Rate | Queue Peak | Best For |
|--------------|------------|----------|------------|----------|
| In-Memory | 18s | 55k logs/sec | Low | Dev/Test |
| File (BoltDB) | 45s | 22k logs/sec | Medium | Single-node |
| Redis | 25s | 40k logs/sec | Low | Distributed |
| SQLite | 60s | 16k logs/sec | High | Local DB |
| PostgreSQL | 50s | 20k logs/sec | Medium | Enterprise |

*Results vary based on hardware, configuration, and workload.*

---

## Recommended Configurations

### Development/Testing

```bash
# Fast feedback loop
go run . \
  -quick \
  -storage memory \
  -export-batch 100 \
  -delay 50ms
```

### Single-Node Production

```bash
# Balanced performance and persistence
go run . \
  -logs 5000000 \
  -storage file \
  -storage-path /var/log/audit/stress_test.db \
  -export-batch 2000 \
  -delay 100ms
```

### Distributed System

```bash
# Multiple instances sharing Redis
go run . \
  -logs 10000000 \
  -storage redis \
  -redis-endpoint redis-cluster:6379 \
  -redis-password "$REDIS_PASSWORD" \
  -export-batch 5000 \
  -delay 50ms
```

### Enterprise/Compliance

```bash
# PostgreSQL with full persistence
go run . \
  -logs 5000000 \
  -storage sql \
  -sql-driver postgres \
  -sql-datasource "postgresql://audituser:$DB_PASSWORD@db:5432/auditdb?sslmode=require" \
  -export-batch 1000 \
  -delay 200ms
```

---

## Storage Backend Selection Guide

### Choose **In-Memory** if:
- ✅ Running quick tests or development
- ✅ Maximum performance needed
- ✅ Persistence not required
- ✅ No external dependencies wanted

### Choose **File** if:
- ✅ Single-node deployment
- ✅ Persistence required
- ✅ No external dependencies wanted
- ✅ Simple setup preferred

### Choose **Redis** if:
- ✅ Multiple instances need shared state
- ✅ High-availability required
- ✅ Distributed system
- ✅ Have Redis infrastructure

### Choose **SQL** if:
- ✅ Enterprise requirements
- ✅ Compliance/audit trail needed
- ✅ Complex querying required
- ✅ Long-term retention
- ✅ Have database infrastructure

---

## Monitoring Storage Performance

### Check Queue Size

The stress test reports queue size during execution:

```
📊 Progress: 500000/1000000 logs (50.0%) | 45000 logs/sec | Queue: 2450 | Errors: 0
```

**Queue Interpretation:**
- **Low (<1000)**: Storage keeping up well
- **Medium (1000-5000)**: Storage slightly behind
- **High (>5000)**: Storage bottleneck, consider:
  - Increasing `-export-batch`
  - Decreasing `-delay`
  - Upgrading storage backend

### Storage-Specific Monitoring

**Redis:**
```bash
redis-cli INFO stats
redis-cli MONITOR
```

**PostgreSQL:**
```sql
SELECT * FROM pg_stat_activity WHERE datname = 'auditdb';
```

**MySQL:**
```sql
SHOW PROCESSLIST;
SHOW ENGINE INNODB STATUS;
```

---

## Troubleshooting

### File Storage: Permission Denied

```bash
# Ensure directory exists and is writable
mkdir -p /var/log/audit
chmod 755 /var/log/audit
```

### Redis: Connection Refused

```bash
# Check Redis is running
redis-cli ping

# Check Redis authentication
redis-cli -a your-password ping
```

### SQL: Connection Failed

```bash
# PostgreSQL
psql -h localhost -U postgres -d auditdb -c "SELECT 1"

# MySQL
mysql -h localhost -u root -p -e "SELECT 1"
```

### Slow Performance

1. **Increase batch size**: `-export-batch 5000`
2. **Decrease delay**: `-delay 25ms`
3. **Check storage capacity**: Disk space, memory, connections
4. **Monitor storage backend**: CPU, I/O, network
5. **Try different storage**: Redis often faster than SQL

---

## Next Steps

1. **Run baseline test** with in-memory storage
2. **Test your target storage** backend
3. **Compare performance** metrics
4. **Tune configuration** based on results
5. **Deploy with optimal** settings

For more information, see the main [README.md](README.md).

