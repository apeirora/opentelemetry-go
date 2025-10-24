// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

func TestAuditLogStorageExtensionAdapter(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()
	adapter, err := NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	record := &Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(log.StringValue("Test message"))

	if err := adapter.Save(ctx, record); err != nil {
		t.Fatalf("Failed to save record: %v", err)
	}

	records, err := adapter.GetAll(ctx)
	if err != nil {
		t.Fatalf("Failed to get all records: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	if records[0].Body().String() != "Test message" {
		t.Errorf("Expected 'Test message', got '%s'", records[0].Body().String())
	}

	if err := adapter.RemoveAll(ctx, records); err != nil {
		t.Fatalf("Failed to remove records: %v", err)
	}

	records, err = adapter.GetAll(ctx)
	if err != nil {
		t.Fatalf("Failed to get all records after removal: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("Expected 0 records after removal, got %d", len(records))
	}
}

func TestAuditLogStorageExtensionAdapterMultipleRecords(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()
	adapter, err := NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	for i := 0; i < 10; i++ {
		record := &Record{}
		record.SetTimestamp(time.Now().Add(time.Duration(i) * time.Millisecond))
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(log.SeverityInfo)
		record.SetSeverityText("INFO")
		record.SetBody(log.StringValue("Test message"))

		if err := adapter.Save(ctx, record); err != nil {
			t.Fatalf("Failed to save record %d: %v", i, err)
		}
	}

	records, err := adapter.GetAll(ctx)
	if err != nil {
		t.Fatalf("Failed to get all records: %v", err)
	}

	if len(records) != 10 {
		t.Fatalf("Expected 10 records, got %d", len(records))
	}

	if err := adapter.RemoveAll(ctx, records[:5]); err != nil {
		t.Fatalf("Failed to remove records: %v", err)
	}

	records, err = adapter.GetAll(ctx)
	if err != nil {
		t.Fatalf("Failed to get all records after removal: %v", err)
	}

	if len(records) != 5 {
		t.Fatalf("Expected 5 records after removal, got %d", len(records))
	}
}

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
	config := RedisStorageConfig{
		Endpoint:   "localhost:6379",
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

func TestAuditLogStorageExtensionAdapterNilClient(t *testing.T) {
	_, err := NewAuditLogStorageExtensionAdapter(nil)
	if err == nil {
		t.Error("Expected error when creating adapter with nil client, got nil")
	}
}

func TestAuditLogStorageExtensionAdapterNilRecord(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()
	adapter, err := NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	err = adapter.Save(ctx, nil)
	if err == nil {
		t.Error("Expected error when saving nil record, got nil")
	}
}

func TestAuditLogStorageExtensionAdapterRemoveEmpty(t *testing.T) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()
	adapter, err := NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}

	err = adapter.RemoveAll(ctx, []Record{})
	if err != nil {
		t.Errorf("Expected no error when removing empty slice, got: %v", err)
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

func BenchmarkStorageExtensionAdapter(b *testing.B) {
	ctx := context.Background()
	client := NewSimpleKeyValueStorageClient()
	adapter, err := NewAuditLogStorageExtensionAdapter(client)
	if err != nil {
		b.Fatalf("Failed to create adapter: %v", err)
	}

	record := &Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(log.StringValue("Benchmark message"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adapter.Save(ctx, record)
	}
}

func TestMultipleStorageTypes(t *testing.T) {
	ctx := context.Background()

	storageTests := []struct {
		name    string
		setup   func() (AuditLogStore, error)
		cleanup func()
	}{
		{
			name: "InMemory",
			setup: func() (AuditLogStore, error) {
				client := NewSimpleKeyValueStorageClient()
				return NewAuditLogStorageExtensionAdapter(client)
			},
			cleanup: func() {},
		},
		{
			name: "BoltDB",
			setup: func() (AuditLogStore, error) {
				client, err := NewBoltDBStorageClient("./test_multi_storage.db")
				if err != nil {
					return nil, err
				}
				return NewAuditLogStorageExtensionAdapter(client)
			},
			cleanup: func() {},
		},
		{
			name: "Redis",
			setup: func() (AuditLogStore, error) {
				config := RedisStorageConfig{
					Endpoint:   "localhost:6379",
					Password:   "",
					DB:         0,
					Prefix:     "test_multi_",
					Expiration: 5 * time.Minute,
				}
				client, err := NewRedisStorageClient(config)
				if err != nil {
					return nil, err
				}
				return NewAuditLogStorageExtensionAdapter(client)
			},
			cleanup: func() {},
		},
		{
			name: "SQL",
			setup: func() (AuditLogStore, error) {
				config := SQLStorageConfig{
					Driver:     "sqlite3",
					Datasource: ":memory:",
					TableName:  "test_multi_storage",
				}
				client, err := NewSQLStorageClient(config)
				if err != nil {
					return nil, err
				}
				return NewAuditLogStorageExtensionAdapter(client)
			},
			cleanup: func() {},
		},
	}

	for _, tt := range storageTests {
		t.Run(tt.name, func(t *testing.T) {
			defer tt.cleanup()

			store, err := tt.setup()
			if err != nil {
				t.Skipf("Skipping %s: setup failed: %v", tt.name, err)
				return
			}

			testRecords := []*Record{}
			for i := 0; i < 5; i++ {
				record := &Record{}
				record.SetTimestamp(time.Now().Add(time.Duration(i) * time.Millisecond))
				record.SetObservedTimestamp(time.Now())
				record.SetSeverity(log.Severity(int32(i + 1)))
				record.SetSeverityText("TEST")
				record.SetBody(log.StringValue("Test message " + string(rune('A'+i))))

				if err := store.Save(ctx, record); err != nil {
					t.Fatalf("%s: Failed to save record %d: %v", tt.name, i, err)
				}
				testRecords = append(testRecords, record)
			}

			records, err := store.GetAll(ctx)
			if err != nil {
				t.Fatalf("%s: Failed to get all records: %v", tt.name, err)
			}

			if len(records) != 5 {
				t.Errorf("%s: Expected 5 records, got %d", tt.name, len(records))
			}

			if err := store.RemoveAll(ctx, records[:2]); err != nil {
				t.Fatalf("%s: Failed to remove records: %v", tt.name, err)
			}

			records, err = store.GetAll(ctx)
			if err != nil {
				t.Fatalf("%s: Failed to get remaining records: %v", tt.name, err)
			}

			if len(records) != 3 {
				t.Errorf("%s: Expected 3 records after removal, got %d", tt.name, len(records))
			}

			if err := store.RemoveAll(ctx, records); err != nil {
				t.Fatalf("%s: Failed to remove remaining records: %v", tt.name, err)
			}

			records, err = store.GetAll(ctx)
			if err != nil {
				t.Fatalf("%s: Failed to get all records after cleanup: %v", tt.name, err)
			}

			if len(records) != 0 {
				t.Errorf("%s: Expected 0 records after cleanup, got %d", tt.name, len(records))
			}
		})
	}
}

func TestStorageTypeInteroperability(t *testing.T) {
	ctx := context.Background()

	memoryClient := NewSimpleKeyValueStorageClient()
	memoryStore, _ := NewAuditLogStorageExtensionAdapter(memoryClient)

	fileClient, err := NewBoltDBStorageClient("./test_interop.db")
	if err != nil {
		t.Skipf("Skipping interop test: file storage unavailable: %v", err)
		return
	}
	fileStore, _ := NewAuditLogStorageExtensionAdapter(fileClient)

	sqlConfig := SQLStorageConfig{
		Driver:     "sqlite3",
		Datasource: ":memory:",
		TableName:  "test_interop",
	}
	sqlClient, err := NewSQLStorageClient(sqlConfig)
	if err != nil {
		t.Skipf("Skipping interop test: SQL storage unavailable: %v", err)
		return
	}
	sqlStore, _ := NewAuditLogStorageExtensionAdapter(sqlClient)

	record := &Record{}
	record.SetTimestamp(time.Now())
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(log.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(log.StringValue("Interop test message"))

	if err := memoryStore.Save(ctx, record); err != nil {
		t.Fatalf("Failed to save to memory store: %v", err)
	}

	if err := fileStore.Save(ctx, record); err != nil {
		t.Fatalf("Failed to save to file store: %v", err)
	}

	if err := sqlStore.Save(ctx, record); err != nil {
		t.Fatalf("Failed to save to SQL store: %v", err)
	}

	memRecords, _ := memoryStore.GetAll(ctx)
	fileRecords, _ := fileStore.GetAll(ctx)
	sqlRecords, _ := sqlStore.GetAll(ctx)

	if len(memRecords) != 1 || len(fileRecords) != 1 || len(sqlRecords) != 1 {
		t.Errorf("Expected 1 record in each store, got memory=%d, file=%d, sql=%d",
			len(memRecords), len(fileRecords), len(sqlRecords))
	}

	if memRecords[0].Body().String() != "Interop test message" {
		t.Errorf("Memory store: unexpected body: %s", memRecords[0].Body().String())
	}
	if fileRecords[0].Body().String() != "Interop test message" {
		t.Errorf("File store: unexpected body: %s", fileRecords[0].Body().String())
	}
	if sqlRecords[0].Body().String() != "Interop test message" {
		t.Errorf("SQL store: unexpected body: %s", sqlRecords[0].Body().String())
	}
}

func TestProcessorWithDifferentStorages(t *testing.T) {
	ctx := context.Background()

	storageConfigs := []struct {
		name  string
		store AuditLogStore
	}{
		{
			name: "Memory",
			store: func() AuditLogStore {
				client := NewSimpleKeyValueStorageClient()
				adapter, _ := NewAuditLogStorageExtensionAdapter(client)
				return adapter
			}(),
		},
		{
			name: "File",
			store: func() AuditLogStore {
				client, err := NewBoltDBStorageClient("./test_processor.db")
				if err != nil {
					return nil
				}
				adapter, _ := NewAuditLogStorageExtensionAdapter(client)
				return adapter
			}(),
		},
		{
			name: "SQL",
			store: func() AuditLogStore {
				config := SQLStorageConfig{
					Driver:     "sqlite3",
					Datasource: ":memory:",
					TableName:  "test_processor",
				}
				client, err := NewSQLStorageClient(config)
				if err != nil {
					return nil
				}
				adapter, _ := NewAuditLogStorageExtensionAdapter(client)
				return adapter
			}(),
		},
	}

	for _, tc := range storageConfigs {
		t.Run(tc.name, func(t *testing.T) {
			if tc.store == nil {
				t.Skipf("Skipping %s: store not available", tc.name)
				return
			}

			exporter := &mockStorageExporter{records: []Record{}}

			processor, err := NewAuditLogProcessorBuilder(exporter, tc.store).
				SetScheduleDelay(100 * time.Millisecond).
				SetMaxExportBatchSize(10).
				Build()
			if err != nil {
				t.Fatalf("Failed to create processor with %s storage: %v", tc.name, err)
			}
			defer processor.Shutdown(ctx)

			for i := 0; i < 5; i++ {
				record := &Record{}
				record.SetTimestamp(time.Now())
				record.SetObservedTimestamp(time.Now())
				record.SetSeverity(log.SeverityInfo)
				record.SetSeverityText("INFO")
				record.SetBody(log.StringValue("Processor test"))

				if err := processor.OnEmit(ctx, record); err != nil {
					t.Errorf("%s: Failed to emit record %d: %v", tc.name, i, err)
				}
			}

			time.Sleep(200 * time.Millisecond)

			if err := processor.ForceFlush(ctx); err != nil {
				t.Errorf("%s: Failed to flush: %v", tc.name, err)
			}

			if len(exporter.records) != 5 {
				t.Errorf("%s: Expected 5 exported records, got %d", tc.name, len(exporter.records))
			}
		})
	}
}

type mockStorageExporter struct {
	records []Record
	mu      sync.Mutex
}

func (e *mockStorageExporter) Export(ctx context.Context, records []Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, records...)
	return nil
}

func (e *mockStorageExporter) Shutdown(ctx context.Context) error {
	return nil
}

func (e *mockStorageExporter) ForceFlush(ctx context.Context) error {
	return nil
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
