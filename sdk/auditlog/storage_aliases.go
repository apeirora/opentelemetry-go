// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"time"

	"go.opentelemetry.io/otel/sdk/auditlog/storage"
)

type StorageClient = storage.StorageClient
type Operation = storage.Operation
type SetOperation = storage.SetOperation
type DeleteOperation = storage.DeleteOperation

type SimpleKeyValueStorageClient = storage.SimpleKeyValueStorageClient
type BoltDBStorageClient = storage.BoltDBStorageClient
type RedisStorageConfig = storage.RedisStorageConfig
type SQLStorageConfig = storage.SQLStorageConfig
type SQLStorageClient = storage.SQLStorageClient
type RealRedisStorageClient = storage.RealRedisStorageClient

type StorageExtension = storage.Extension
type StorageExtensionConfig = storage.ExtensionConfig
type MemoryStorageExtension = storage.MemoryExtension
type FileStorageConfig = storage.FileConfig
type FileStorageExtension = storage.FileExtension
type RedisStorageExtension = storage.RedisExtension
type SQLStorageExtension = storage.SQLExtension

type StorageType = storage.Type
type StorageFactory = storage.Factory
type StorageFactoryOption = storage.FactoryOption
type RedisOption = storage.RedisOption
type SQLOption = storage.SQLOption

const (
	StorageTypeMemory StorageType = storage.TypeMemory
	StorageTypeFile   StorageType = storage.TypeFile
	StorageTypeRedis  StorageType = storage.TypeRedis
	StorageTypeSQL    StorageType = storage.TypeSQL
)

func NewSimpleKeyValueStorageClient() *SimpleKeyValueStorageClient {
	return storage.NewSimpleKeyValueStorageClient()
}

func NewBoltDBStorageClient(filePath string) (*BoltDBStorageClient, error) {
	return storage.NewBoltDBStorageClient(filePath)
}

func NewRedisStorageClient(config RedisStorageConfig) (StorageClient, error) {
	return storage.NewRedisStorageClient(config)
}

func NewSQLStorageClient(config SQLStorageConfig) (*SQLStorageClient, error) {
	return storage.NewSQLStorageClient(config)
}

func NewRealRedisStorageClient(config RedisStorageConfig) (*RealRedisStorageClient, error) {
	return storage.NewRealRedisStorageClient(config)
}

func NewMemoryStorageExtension() *MemoryStorageExtension {
	return storage.NewMemoryExtension()
}

func NewFileStorageExtension(config *FileStorageConfig) (*FileStorageExtension, error) {
	return storage.NewFileExtension(config)
}

func NewRedisStorageExtension(config RedisStorageConfig) (*RedisStorageExtension, error) {
	return storage.NewRedisExtension(config)
}

func NewSQLStorageExtension(config SQLStorageConfig) (*SQLStorageExtension, error) {
	return storage.NewSQLExtension(config)
}

func WithMemoryStorage() StorageFactoryOption {
	return storage.WithMemory()
}

func WithFileStorage(directory string) StorageFactoryOption {
	return storage.WithFile(directory)
}

func WithRedisStorage(endpoint string, opts ...RedisOption) StorageFactoryOption {
	return storage.WithRedis(endpoint, opts...)
}

func WithRedisPassword(password string) RedisOption {
	return storage.WithRedisPassword(password)
}

func WithRedisDB(db int) RedisOption {
	return storage.WithRedisDB(db)
}

func WithRedisPrefix(prefix string) RedisOption {
	return storage.WithRedisPrefix(prefix)
}

func WithRedisExpiration(expiration time.Duration) RedisOption {
	return storage.WithRedisExpiration(expiration)
}

func WithSQLStorage(driver, datasource string, opts ...SQLOption) StorageFactoryOption {
	return storage.WithSQL(driver, datasource, opts...)
}

func WithSQLTableName(tableName string) SQLOption {
	return storage.WithSQLTableName(tableName)
}

func NewStorageExtension(opts ...StorageFactoryOption) (StorageExtension, error) {
	return storage.NewExtension(opts...)
}
