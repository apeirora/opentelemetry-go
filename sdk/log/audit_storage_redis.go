// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RealRedisStorageClient struct {
	client     *redis.Client
	prefix     string
	expiration time.Duration
}

func NewRealRedisStorageClient(config RedisStorageConfig) (*RealRedisStorageClient, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.Endpoint,
		Password: config.Password,
		DB:       config.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis at %s: %w", config.Endpoint, err)
	}

	return &RealRedisStorageClient{
		client:     client,
		prefix:     config.Prefix,
		expiration: config.Expiration,
	}, nil
}

func (c *RealRedisStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	fullKey := c.prefix + key
	
	result, err := c.client.Get(ctx, fullKey).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return result, nil
}

func (c *RealRedisStorageClient) Set(ctx context.Context, key string, value []byte) error {
	fullKey := c.prefix + key

	expiration := c.expiration
	if expiration == 0 {
		expiration = 24 * time.Hour
	}

	if err := c.client.Set(ctx, fullKey, value, expiration).Err(); err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}

	return nil
}

func (c *RealRedisStorageClient) Delete(ctx context.Context, key string) error {
	fullKey := c.prefix + key

	if err := c.client.Del(ctx, fullKey).Err(); err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

func (c *RealRedisStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	pipe := c.client.Pipeline()

	for _, op := range ops {
		switch o := op.(type) {
		case *SetOperation:
			fullKey := c.prefix + o.Key
			expiration := c.expiration
			if expiration == 0 {
				expiration = 24 * time.Hour
			}
			pipe.Set(ctx, fullKey, o.Value, expiration)
		case *DeleteOperation:
			fullKey := c.prefix + o.Key
			pipe.Del(ctx, fullKey)
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to execute batch operations: %w", err)
	}

	return nil
}

func (c *RealRedisStorageClient) Close(ctx context.Context) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

