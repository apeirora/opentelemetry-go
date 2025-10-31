# OpenTelemetry Go Audit Log Feature - Usage Guide

## Overview

Your OpenTelemetry Go SDK implementation includes a comprehensive audit logging system that provides **persistent storage**, **priority-based processing**, and **retry mechanisms** - features that go beyond the standard OpenTelemetry Java SDK implementation.

## 🚀 Quick Start

### 1. Basic Setup

```go
package main

import (
    "context"
    "time"
    "go.opentelemetry.io/otel/sdk/log"
)

func main() {
    // Create persistent audit log store
    store, err := log.NewAuditLogFileStore("/var/log/audit")
    if err != nil {
        log.Fatal(err)
    }
    
    // Create your exporter (e.g., OTLP, file, etc.)
    exporter := &YourExporter{}
    
    // Build audit processor with configuration
    processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
        SetScheduleDelay(time.Second).           // Export every second
        SetMaxExportBatchSize(100).              // Batch size
        SetExporterTimeout(30 * time.Second).    // Timeout
        SetRetryPolicy(log.RetryPolicy{
            MaxAttempts:       3,
            InitialBackoff:    time.Second,
            MaxBackoff:        time.Minute,
            BackoffMultiplier: 2.0,
        }).
        Build()
    
    // Create logger
    logger := log.NewLogger(processor)
    
    // Emit audit records
    record := &log.Record{
        Timestamp: time.Now(),
        Severity:  log.SeverityInfo,
        Body:      log.StringValue("User action performed"),
    }
    record.AddAttributes(log.String("user_id", "12345"))
    
    logger.Emit(context.Background(), record)
}
```

### 2. Key Features

| Feature | Description | Benefit |
|---------|-------------|---------|
| **Persistent Storage** | File-based storage survives restarts | No audit log loss |
| **Priority Processing** | Higher severity logs processed first | Security events prioritized |
| **Retry Logic** | Exponential backoff for failed exports | Reliable delivery |
| **Batch Processing** | Configurable batch sizes | Efficient throughput |
| **Exception Handling** | Custom error handling | Comprehensive monitoring |
| **Thread Safety** | Concurrent access support | Production ready |

## 📋 Usage Examples

### Security Audit Logging

```go
// Critical security event - processed with highest priority
securityRecord := &log.Record{
    Timestamp: time.Now(),
    Severity:  log.SeverityError,  // High priority
    Body:      log.StringValue("SECURITY: Unauthorized access attempt"),
}
securityRecord.AddAttributes(
    log.String("user_id", "hacker123"),
    log.String("ip_address", "192.168.1.100"),
    log.String("event_type", "security_breach"),
)
logger.Emit(ctx, securityRecord)
```

### User Activity Logging

```go
// User login event
loginRecord := &log.Record{
    Timestamp: time.Now(),
    Severity:  log.SeverityInfo,
    Body:      log.StringValue("USER: Successful login"),
}
loginRecord.AddAttributes(
    log.String("user_id", "john.doe"),
    log.String("session_id", "sess_abc123"),
    log.String("login_method", "password"),
)
logger.Emit(ctx, loginRecord)
```

### Data Access Logging

```go
// Sensitive data access
dataRecord := &log.Record{
    Timestamp: time.Now(),
    Severity:  log.SeverityWarn,
    Body:      log.StringValue("DATA: Sensitive data accessed"),
}
dataRecord.AddAttributes(
    log.String("user_id", "jane.smith"),
    log.String("data_type", "customer_pii"),
    log.String("operation", "read"),
    log.String("record_count", "150"),
)
logger.Emit(ctx, dataRecord)
```

## 🔧 Configuration Options

### Processor Configuration

```go
processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
    SetScheduleDelay(2 * time.Second).           // Export frequency
    SetMaxExportBatchSize(512).                  // Batch size
    SetExporterTimeout(30 * time.Second).        // Export timeout
    SetWaitOnExport(false).                      // Non-blocking
    SetRetryPolicy(log.RetryPolicy{
        MaxAttempts:       3,                    // Retry attempts
        InitialBackoff:    time.Second,          // Initial delay
        MaxBackoff:        time.Minute,          // Max delay
        BackoffMultiplier: 2.0,                  // Backoff multiplier
    }).
    SetExceptionHandler(&CustomHandler{}).       // Custom error handling
    Build()
```

### Storage Options

```go
// File-based persistent storage
fileStore, err := log.NewAuditLogFileStore("/var/log/audit")

// In-memory storage for testing
memoryStore := log.NewAuditLogInMemoryStore()
```

## 🆚 Comparison with Java SDK

### Your Go Implementation vs Java SDK

| Feature | Go SDK (Your Implementation) | Java SDK (Standard) |
|---------|------------------------------|---------------------|
| **Storage** | ✅ Persistent file storage | ❌ Memory-only |
| **Priority** | ✅ Severity-based priority queue | ❌ FIFO processing |
| **Recovery** | ✅ Loads existing logs on restart | ❌ Starts fresh |
| **Deduplication** | ✅ Prevents duplicate entries | ❌ No deduplication |
| **Exception Handling** | ✅ Structured audit exceptions | ❌ Standard exceptions |
| **Thread Safety** | ✅ Mutex-based synchronization | ✅ Built-in concurrency |
| **Performance** | ✅ Configurable batch processing | ✅ Optimized for throughput |
| **Ecosystem** | ❌ Limited Go ecosystem | ✅ Rich Java ecosystem |

### When to Use Your Go Implementation

**✅ Choose Your Go Implementation When:**
- **Compliance requirements** demand persistent audit logs
- **Security-critical** applications need priority processing
- **Data integrity** is paramount (no log loss)
- **Custom error handling** is required
- **File-based storage** is acceptable

**✅ Choose Java SDK When:**
- **High throughput** is required
- **Java ecosystem integration** is important
- **Standard patterns** are preferred
- **Memory-only processing** is acceptable

## 🏗️ Architecture

### Component Overview

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Application   │───▶│  AuditLogStore   │───▶│   File System   │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │
         ▼                       ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ AuditLogProcessor│◀───│ Priority Queue   │───▶│    Exporter     │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │
         ▼                       ▼
┌─────────────────┐    ┌──────────────────┐
│Exception Handler│    │   Retry Logic    │
└─────────────────┘    └──────────────────┘
```

### Priority Processing Flow

1. **Log Emission**: Application emits audit log
2. **Persistence**: Log saved to file store
3. **Priority Queue**: Log added to priority queue (severity-based)
4. **Batch Processing**: Logs exported in batches
5. **Retry Logic**: Failed exports retried with backoff
6. **Cleanup**: Successful exports removed from storage

## 🧪 Testing

### Unit Testing with In-Memory Store

```go
func TestAuditLogging(t *testing.T) {
    // Use in-memory store for testing
    store := log.NewAuditLogInMemoryStore()
    exporter := &MockExporter{}
    
    processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
        SetScheduleDelay(10 * time.Millisecond).
        Build()
    
    // Test audit logging
    record := createTestAuditRecord("test event", log.SeverityInfo)
    processor.OnEmit(context.Background(), &record)
    
    // Verify export
    time.Sleep(50 * time.Millisecond)
    if exporter.GetExportCount() == 0 {
        t.Error("Expected record to be exported")
    }
}
```

## 🔒 Security Considerations

### File Permissions
```bash
# Set appropriate permissions for audit logs
chmod 640 /var/log/audit/audit.log
chown root:audit /var/log/audit/audit.log
```

### Log Rotation
```bash
# Configure log rotation to prevent disk space issues
# /etc/logrotate.d/audit
/var/log/audit/audit.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 640 root audit
}
```

## 📊 Monitoring

### Queue Monitoring
```go
// Monitor queue size
queueSize := processor.GetQueueSize()
if queueSize > 1000 {
    // Alert: Queue is getting large
}

// Monitor retry attempts
retryAttempts := processor.GetRetryAttempts()
if retryAttempts > 0 {
    // Alert: Export failures detected
}
```

### Exception Monitoring
```go
type MonitoringExceptionHandler struct {
    alertService AlertService
}

func (h *MonitoringExceptionHandler) Handle(exception *log.AuditException) {
    // Send alert for audit failures
    h.alertService.SendAlert(fmt.Sprintf(
        "Audit log export failed: %s", exception.Message))
}
```

## 🚀 Production Deployment

### 1. Configuration
```go
// Production configuration
processor, err := log.NewAuditLogProcessorBuilder(exporter, store).
    SetScheduleDelay(5 * time.Second).           // Less frequent for production
    SetMaxExportBatchSize(1000).                 // Larger batches
    SetExporterTimeout(60 * time.Second).        // Longer timeout
    SetRetryPolicy(log.RetryPolicy{
        MaxAttempts:       5,                    // More retry attempts
        InitialBackoff:    2 * time.Second,
        MaxBackoff:        5 * time.Minute,
        BackoffMultiplier: 2.0,
    }).
    Build()
```

### 2. Health Checks
```go
// Implement health check endpoint
func healthCheck() bool {
    return processor.GetQueueSize() < 10000 && 
           processor.GetRetryAttempts() < 10
}
```

### 3. Graceful Shutdown
```go
// Ensure all logs are exported on shutdown
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    processor.Shutdown(ctx)
}()
```

## 📚 Additional Resources

- **README**: `sdk/log/AUDIT_LOG_README.md` - Detailed documentation
- **Tests**: `sdk/log/audit_processor_test.go` - Comprehensive test suite
- **Examples**: `sdk/log/example_usage.go` - Usage examples
- **Comparison**: `AUDIT_LOG_COMPARISON.md` - Go vs Java SDK comparison

## 🎯 Key Takeaways

1. **Your Go implementation provides audit-specific enhancements** not found in the standard Java SDK
2. **Persistent storage ensures no audit log loss** - critical for compliance
3. **Priority processing prioritizes security events** - important for security monitoring
4. **Comprehensive retry logic ensures reliable delivery** - essential for production
5. **Thread-safe design supports concurrent access** - production-ready architecture

Your implementation is particularly valuable for **security auditing**, **compliance-critical applications**, and **systems requiring log persistence** - making it superior to the Java SDK for audit-specific use cases.

