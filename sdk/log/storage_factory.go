// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"fmt"
	"time"
)

type StorageType string

const (
	StorageTypeMemory StorageType = "memory"
	StorageTypeFile   StorageType = "file"
	StorageTypeRedis  StorageType = "redis"
	StorageTypeSQL    StorageType = "sql"
)

type StorageFactory struct {
	storageType StorageType
	config      interface{}
}

type StorageFactoryOption func(*StorageFactory)

func WithMemoryStorage() StorageFactoryOption {
	return func(f *StorageFactory) {
		f.storageType = StorageTypeMemory
	}
}

func WithFileStorage(directory string) StorageFactoryOption {
	return func(f *StorageFactory) {
		f.storageType = StorageTypeFile
		f.config = &FileStorageConfig{
			Directory: directory,
		}
	}
}

func WithRedisStorage(endpoint string, opts ...RedisOption) StorageFactoryOption {
	return func(f *StorageFactory) {
		f.storageType = StorageTypeRedis
		config := RedisStorageConfig{
			Endpoint:   endpoint,
			Prefix:     "otel_",
			Expiration: 24 * time.Hour,
		}
		
		for _, opt := range opts {
			opt(&config)
		}
		
		f.config = config
	}
}

func WithSQLStorage(driver, datasource string, opts ...SQLOption) StorageFactoryOption {
	return func(f *StorageFactory) {
		f.storageType = StorageTypeSQL
		config := SQLStorageConfig{
			Driver:     driver,
			Datasource: datasource,
			TableName:  "otel_storage",
		}
		
		for _, opt := range opts {
			opt(&config)
		}
		
		f.config = config
	}
}

type RedisOption func(*RedisStorageConfig)

func WithRedisPassword(password string) RedisOption {
	return func(c *RedisStorageConfig) {
		c.Password = password
	}
}

func WithRedisDB(db int) RedisOption {
	return func(c *RedisStorageConfig) {
		c.DB = db
	}
}

func WithRedisPrefix(prefix string) RedisOption {
	return func(c *RedisStorageConfig) {
		c.Prefix = prefix
	}
}

func WithRedisExpiration(expiration time.Duration) RedisOption {
	return func(c *RedisStorageConfig) {
		c.Expiration = expiration
	}
}

type SQLOption func(*SQLStorageConfig)

func WithSQLTableName(tableName string) SQLOption {
	return func(c *SQLStorageConfig) {
		c.TableName = tableName
	}
}

func NewStorageExtension(opts ...StorageFactoryOption) (StorageExtension, error) {
	factory := &StorageFactory{
		storageType: StorageTypeMemory,
	}
	
	for _, opt := range opts {
		opt(factory)
	}
	
	switch factory.storageType {
	case StorageTypeMemory:
		return NewMemoryStorageExtension(), nil
		
	case StorageTypeFile:
		config, ok := factory.config.(*FileStorageConfig)
		if !ok {
			return nil, fmt.Errorf("invalid file storage configuration")
		}
		return NewFileStorageExtension(config)
		
	case StorageTypeRedis:
		config, ok := factory.config.(RedisStorageConfig)
		if !ok {
			return nil, fmt.Errorf("invalid Redis storage configuration")
		}
		return NewRedisStorageExtension(config)
		
	case StorageTypeSQL:
		config, ok := factory.config.(SQLStorageConfig)
		if !ok {
			return nil, fmt.Errorf("invalid SQL storage configuration")
		}
		return NewSQLStorageExtension(config)
		
	default:
		return nil, fmt.Errorf("unknown storage type: %s", factory.storageType)
	}
}

