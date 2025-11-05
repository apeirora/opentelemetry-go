// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"fmt"
	"time"
)

type StorageOption interface {
	apply(*storageConfig)
}

type storageConfig struct {
	storageType StorageType
	extension   StorageExtension
	clientName  string
	
	fileDirectory string
	
	redisEndpoint   string
	redisPassword   string
	redisDB         int
	redisPrefix     string
	redisExpiration time.Duration
	
	sqlDriver     string
	sqlDatasource string
	sqlTableName  string
}

func (b *AuditLogProcessorBuilder) WithMemoryStorage() *AuditLogProcessorBuilder {
	b.storageConfig = &storageConfig{
		storageType: StorageTypeMemory,
		clientName:  "audit_processor",
	}
	return b
}

func (b *AuditLogProcessorBuilder) WithFileStorage(directory string) *AuditLogProcessorBuilder {
	b.storageConfig = &storageConfig{
		storageType:   StorageTypeFile,
		fileDirectory: directory,
		clientName:    "audit_processor",
	}
	return b
}

type RedisStorageOption func(*storageConfig)

func WithRedisEndpoint(endpoint string) RedisStorageOption {
	return func(c *storageConfig) {
		c.redisEndpoint = endpoint
	}
}

func WithRedisAuth(password string, db int) RedisStorageOption {
	return func(c *storageConfig) {
		c.redisPassword = password
		c.redisDB = db
	}
}

func WithRedisKeyPrefix(prefix string) RedisStorageOption {
	return func(c *storageConfig) {
		c.redisPrefix = prefix
	}
}

func WithRedisKeyExpiration(expiration time.Duration) RedisStorageOption {
	return func(c *storageConfig) {
		c.redisExpiration = expiration
	}
}

func (b *AuditLogProcessorBuilder) WithRedisStorage(opts ...RedisStorageOption) *AuditLogProcessorBuilder {
	b.storageConfig = &storageConfig{
		storageType:     StorageTypeRedis,
		redisEndpoint:   "localhost:6379",
		redisPrefix:     "otel_audit_",
		redisExpiration: 24 * time.Hour,
		clientName:      "audit_processor",
	}
	
	for _, opt := range opts {
		opt(b.storageConfig)
	}
	
	return b
}

type SQLStorageOption func(*storageConfig)

func WithSQLDriver(driver string) SQLStorageOption {
	return func(c *storageConfig) {
		c.sqlDriver = driver
	}
}

func WithSQLDatasource(datasource string) SQLStorageOption {
	return func(c *storageConfig) {
		c.sqlDatasource = datasource
	}
}

func WithSQLTable(tableName string) SQLStorageOption {
	return func(c *storageConfig) {
		c.sqlTableName = tableName
	}
}

func (b *AuditLogProcessorBuilder) WithSQLStorage(opts ...SQLStorageOption) *AuditLogProcessorBuilder {
	b.storageConfig = &storageConfig{
		storageType:  StorageTypeSQL,
		sqlDriver:    "sqlite3",
		sqlDatasource: ":memory:",
		sqlTableName: "audit_logs",
		clientName:   "audit_processor",
	}
	
	for _, opt := range opts {
		opt(b.storageConfig)
	}
	
	return b
}

func (b *AuditLogProcessorBuilder) createStorageExtension() (StorageExtension, error) {
	if b.storageConfig == nil {
		return NewStorageExtension(WithMemoryStorage())
	}
	
	switch b.storageConfig.storageType {
	case StorageTypeMemory:
		return NewStorageExtension(WithMemoryStorage())
		
	case StorageTypeFile:
		if b.storageConfig.fileDirectory == "" {
			return nil, fmt.Errorf("file storage directory cannot be empty")
		}
		return NewStorageExtension(WithFileStorage(b.storageConfig.fileDirectory))
		
	case StorageTypeRedis:
		if b.storageConfig.redisEndpoint == "" {
			return nil, fmt.Errorf("Redis endpoint cannot be empty")
		}
		
		var opts []RedisOption
		if b.storageConfig.redisPassword != "" {
			opts = append(opts, WithRedisPassword(b.storageConfig.redisPassword))
		}
		if b.storageConfig.redisDB != 0 {
			opts = append(opts, WithRedisDB(b.storageConfig.redisDB))
		}
		if b.storageConfig.redisPrefix != "" {
			opts = append(opts, WithRedisPrefix(b.storageConfig.redisPrefix))
		}
		if b.storageConfig.redisExpiration > 0 {
			opts = append(opts, WithRedisExpiration(b.storageConfig.redisExpiration))
		}
		
		return NewStorageExtension(
			WithRedisStorage(b.storageConfig.redisEndpoint, opts...),
		)
		
	case StorageTypeSQL:
		if b.storageConfig.sqlDriver == "" {
			return nil, fmt.Errorf("SQL driver cannot be empty")
		}
		if b.storageConfig.sqlDatasource == "" {
			return nil, fmt.Errorf("SQL datasource cannot be empty")
		}
		
		var opts []SQLOption
		if b.storageConfig.sqlTableName != "" {
			opts = append(opts, WithSQLTableName(b.storageConfig.sqlTableName))
		}
		
		return NewStorageExtension(
			WithSQLStorage(b.storageConfig.sqlDriver, b.storageConfig.sqlDatasource, opts...),
		)
		
	default:
		return nil, fmt.Errorf("unknown storage type: %s", b.storageConfig.storageType)
	}
}

