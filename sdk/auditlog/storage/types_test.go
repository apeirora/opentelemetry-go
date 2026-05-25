// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"path/filepath"
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

	if client.Size() != 3 {
		t.Errorf("Expected 3 keys after batch, got %d", client.Size())
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

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}
}

func TestRedisStorageClientNoExpirationByDefault(t *testing.T) {
	ctx := context.Background()
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start test Redis server: %v", err)
	}
	defer redisServer.Close()

	client, err := NewRedisStorageClient(RedisStorageConfig{
		Endpoint: redisServer.Addr(),
		Prefix:   "persist_",
	})
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close(ctx)

	if err := client.Set(ctx, "k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if redisServer.TTL("persist_k") != 0 {
		t.Fatalf("expected no TTL on audit key by default, got %v", redisServer.TTL("persist_k"))
	}
}

func TestSQLStorageClient(t *testing.T) {
	ctx := context.Background()
	config := SQLStorageConfig{
		Driver:     "sqlite",
		Datasource: "file:" + filepath.Join(t.TempDir(), "audit.db") + "?_pragma=foreign_keys(1)",
		TableName:  "audit_logs",
	}

	client, err := NewSQLStorageClient(config)
	if err != nil {
		t.Fatalf("Failed to create SQL client: %v", err)
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

	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	_, err = client.Get(ctx, key)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSQLStorageClientPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "reopen.db")
	config := SQLStorageConfig{
		Driver:     "sqlite",
		Datasource: "file:" + dbPath,
		TableName:  "audit_logs",
	}

	first, err := NewSQLStorageClient(config)
	if err != nil {
		t.Fatalf("create first client: %v", err)
	}
	if err := first.Set(ctx, "persist", []byte("yes")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := NewSQLStorageClient(config)
	if err != nil {
		t.Fatalf("create second client: %v", err)
	}
	defer second.Close(ctx)

	got, err := second.Get(ctx, "persist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "yes" {
		t.Fatalf("expected persisted value, got %q", got)
	}
}

func TestBoltDBStorageClient(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "audit.db")

	client, err := NewBoltDBStorageClient(dbPath)
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

func TestBoltDBStorageClientPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "reopen.db")

	first, err := NewBoltDBStorageClient(dbPath)
	if err != nil {
		t.Fatalf("create first client: %v", err)
	}
	if err := first.Set(ctx, "persist", []byte("yes")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := NewBoltDBStorageClient(dbPath)
	if err != nil {
		t.Fatalf("create second client: %v", err)
	}
	defer second.Close(ctx)

	got, err := second.Get(ctx, "persist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "yes" {
		t.Fatalf("expected persisted value, got %q", got)
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
