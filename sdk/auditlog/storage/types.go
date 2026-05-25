// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type StorageClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Batch(ctx context.Context, ops ...Operation) error
	Close(ctx context.Context) error
}

type Operation interface {
	Execute(ctx context.Context) error
}

type SetOperation struct {
	Key   string
	Value []byte
}

func (op *SetOperation) Execute(context.Context) error {
	return fmt.Errorf("SetOperation.Execute requires a StorageClient.Batch call")
}

type DeleteOperation struct {
	Key string
}

func (op *DeleteOperation) Execute(context.Context) error {
	return fmt.Errorf("DeleteOperation.Execute requires a StorageClient.Batch call")
}

type SimpleKeyValueStorageClient struct {
	storage map[string][]byte
	mutex   sync.RWMutex
}

func NewSimpleKeyValueStorageClient() *SimpleKeyValueStorageClient {
	return &SimpleKeyValueStorageClient{storage: make(map[string][]byte)}
}

func (c *SimpleKeyValueStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	value, exists := c.storage[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

func (c *SimpleKeyValueStorageClient) Set(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.storage[key] = valueCopy
	return nil
}

func (c *SimpleKeyValueStorageClient) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.storage, key)
	return nil
}

func (c *SimpleKeyValueStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch o := op.(type) {
		case *SetOperation:
			valueCopy := make([]byte, len(o.Value))
			copy(valueCopy, o.Value)
			c.storage[o.Key] = valueCopy
		case *DeleteOperation:
			delete(c.storage, o.Key)
		}
	}
	return nil
}

func (c *SimpleKeyValueStorageClient) Close(context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.storage = make(map[string][]byte)
	return nil
}

func (c *SimpleKeyValueStorageClient) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.storage = make(map[string][]byte)
}

func (c *SimpleKeyValueStorageClient) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.storage)
}

type RedisStorageConfig struct {
	Endpoint   string
	Password   string
	DB         int
	Prefix     string
	Expiration time.Duration
}

func NewRedisStorageClient(config RedisStorageConfig) (StorageClient, error) {
	return NewRealRedisStorageClient(config)
}

type SQLStorageConfig struct {
	Driver     string
	Datasource string
	TableName  string
}
