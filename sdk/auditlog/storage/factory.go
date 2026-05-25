// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"fmt"
	"time"
)

type Type string

const (
	TypeMemory Type = "memory"
	TypeFile   Type = "file"
	TypeRedis  Type = "redis"
	TypeSQL    Type = "sql"
)

type Factory struct {
	storageType Type
	config      interface{}
}

type FactoryOption func(*Factory)

func WithMemory() FactoryOption {
	return func(f *Factory) { f.storageType = TypeMemory }
}

func WithFile(directory string) FactoryOption {
	return func(f *Factory) {
		f.storageType = TypeFile
		f.config = &FileConfig{Directory: directory}
	}
}

type RedisOption func(*RedisStorageConfig)

func WithRedis(endpoint string, opts ...RedisOption) FactoryOption {
	return func(f *Factory) {
		f.storageType = TypeRedis
		config := RedisStorageConfig{Endpoint: endpoint, Prefix: "otel_"}
		for _, opt := range opts {
			opt(&config)
		}
		f.config = config
	}
}

func WithRedisPassword(password string) RedisOption {
	return func(c *RedisStorageConfig) { c.Password = password }
}

func WithRedisDB(db int) RedisOption {
	return func(c *RedisStorageConfig) { c.DB = db }
}

func WithRedisPrefix(prefix string) RedisOption {
	return func(c *RedisStorageConfig) { c.Prefix = prefix }
}

func WithRedisExpiration(expiration time.Duration) RedisOption {
	return func(c *RedisStorageConfig) { c.Expiration = expiration }
}

type SQLOption func(*SQLStorageConfig)

func WithSQL(driver, datasource string, opts ...SQLOption) FactoryOption {
	return func(f *Factory) {
		f.storageType = TypeSQL
		config := SQLStorageConfig{Driver: driver, Datasource: datasource, TableName: "otel_storage"}
		for _, opt := range opts {
			opt(&config)
		}
		f.config = config
	}
}

func WithSQLTableName(tableName string) SQLOption {
	return func(c *SQLStorageConfig) { c.TableName = tableName }
}

func NewExtension(opts ...FactoryOption) (Extension, error) {
	factory := &Factory{storageType: TypeMemory}
	for _, opt := range opts {
		opt(factory)
	}
	switch factory.storageType {
	case TypeMemory:
		return NewMemoryExtension(), nil
	case TypeFile:
		config, ok := factory.config.(*FileConfig)
		if !ok {
			return nil, fmt.Errorf("invalid file storage configuration")
		}
		return NewFileExtension(config)
	case TypeRedis:
		config, ok := factory.config.(RedisStorageConfig)
		if !ok {
			return nil, fmt.Errorf("invalid redis storage configuration")
		}
		return NewRedisExtension(config)
	case TypeSQL:
		config, ok := factory.config.(SQLStorageConfig)
		if !ok {
			return nil, fmt.Errorf("invalid sql storage configuration")
		}
		return NewSQLExtension(config)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", factory.storageType)
	}
}
