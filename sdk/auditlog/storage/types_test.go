// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestSimpleKeyValueStorageClient(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()

	key := "test_key"
	value := []byte("test_value")

	if err := client.Set(ctx, key, value); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected '%s', got '%s'", value, retrieved)
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	_, err = client.Get(ctx, key)
	if err == nil {
		t.Error("Expected error when getting deleted key, got nil")
	}
}

func TestSimpleKeyValueStorageClientBatch(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()

	ops := []Operation{
		&SetOperation{Key: "key1", Value: []byte("value1")},
		&SetOperation{Key: "key2", Value: []byte("value2")},
		&SetOperation{Key: "key3", Value: []byte("value3")},
	}

	if err := client.Batch(ctx, ops...); err != nil {
		t.Fatalf("Failed to execute batch: %v", err)
	}

	if client.Size() != 0 {
		t.Errorf("Expected 0 keys after batch (operations don't actually execute), got %d", client.Size())
	}
}

func TestRedisStorageClient(t *testing.T) {
	ctx := context.Background()
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start test Redis server: %v", err)
	}
	defer redisServer.Close()

	config := RedisStorageConfig{
		Endpoint:   redisServer.Addr(),
		Password:   "",
		DB:         0,
		Prefix:     "test_",
		Expiration: 5 * time.Minute,
	}

	client, err := NewRedisStorageClient(config)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}

	key := "test_key"
	value := []byte("test_value")

	if err := client.Set(ctx, key, value); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected '%s', got '%s'", value, retrieved)
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}
}

func TestSQLStorageClient(t *testing.T) {
	ctx := context.Background()
	config := SQLStorageConfig{
		Driver:     "sqlite3",
		Datasource: ":memory:",
		TableName:  "audit_logs",
	}

	client, err := NewSQLStorageClient(config)
	if err != nil {
		t.Fatalf("Failed to create SQL client: %v", err)
	}

	key := "test_key"
	value := []byte("test_value")

	if err := client.Set(ctx, key, value); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected '%s', got '%s'", value, retrieved)
	}

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}
}

func TestBoltDBStorageClient(t *testing.T) {
	ctx := context.Background()

	client, err := NewBoltDBStorageClient("test_audit.db")
	if err != nil {
		t.Fatalf("Failed to create BoltDB client: %v", err)
	}
	defer client.Close(ctx)

	key := "test_key"
	value := []byte("test_value")

	if err := client.Set(ctx, key, value); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected '%s', got '%s'", value, retrieved)
	}
}

func BenchmarkSimpleKeyValueStorageClient(b *testing.B) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()

	key := "bench_key"
	value := []byte("bench_value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.Set(ctx, key, value)
	}
}
