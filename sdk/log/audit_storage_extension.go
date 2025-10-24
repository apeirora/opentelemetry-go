// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/log"
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

func (op *SetOperation) Execute(ctx context.Context) error {
	return nil
}

type DeleteOperation struct {
	Key string
}

func (op *DeleteOperation) Execute(ctx context.Context) error {
	return nil
}

type recordData struct {
	Timestamp         time.Time
	ObservedTimestamp time.Time
	Severity          int32
	SeverityText      string
	Body              string
	BodyKind          int
	Attributes        []keyValuePair
	TraceID           []byte
	SpanID            []byte
	TraceFlags        uint8
}

type keyValuePair struct {
	Key   string
	Value string
	Kind  int
}

type AuditLogStorageExtensionAdapter struct {
	client StorageClient
	mutex  sync.RWMutex

	indexKey string
}

func NewAuditLogStorageExtensionAdapter(client StorageClient) (*AuditLogStorageExtensionAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("storage client cannot be nil")
	}

	adapter := &AuditLogStorageExtensionAdapter{
		client:   client,
		indexKey: "audit_log_index",
	}

	return adapter, nil
}

func (s *AuditLogStorageExtensionAdapter) serializeRecord(record *Record) ([]byte, error) {
	data := recordData{
		Timestamp:         record.Timestamp(),
		ObservedTimestamp: record.ObservedTimestamp(),
		Severity:          int32(record.Severity()),
		SeverityText:      record.SeverityText(),
		Body:              record.Body().String(),
		BodyKind:          int(record.Body().Kind()),
	}

	record.WalkAttributes(func(kv log.KeyValue) bool {
		data.Attributes = append(data.Attributes, keyValuePair{
			Key:   string(kv.Key),
			Value: kv.Value.AsString(),
			Kind:  int(kv.Value.Kind()),
		})
		return true
	})

	traceID := record.TraceID()
	spanID := record.SpanID()
	data.TraceID = traceID[:]
	data.SpanID = spanID[:]
	data.TraceFlags = uint8(record.TraceFlags())

	return json.Marshal(data)
}

func (s *AuditLogStorageExtensionAdapter) deserializeRecord(data []byte) (*Record, error) {
	var rd recordData
	if err := json.Unmarshal(data, &rd); err != nil {
		return nil, err
	}

	record := &Record{}
	record.SetTimestamp(rd.Timestamp)
	record.SetObservedTimestamp(rd.ObservedTimestamp)
	record.SetSeverity(log.Severity(rd.Severity))
	record.SetSeverityText(rd.SeverityText)
	record.SetBody(log.StringValue(rd.Body))

	return record, nil
}

func (s *AuditLogStorageExtensionAdapter) Save(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	recordID := s.generateRecordID(record)
	recordKey := fmt.Sprintf("audit_record_%s", recordID)

	data, err := s.serializeRecord(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	if err := s.client.Set(ctx, recordKey, data); err != nil {
		return fmt.Errorf("failed to save record: %w", err)
	}

	if err := s.addToIndex(ctx, recordID); err != nil {
		return fmt.Errorf("failed to add record to index: %w", err)
	}

	return nil
}

func (s *AuditLogStorageExtensionAdapter) RemoveAll(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, record := range records {
		recordID := s.generateRecordID(&record)
		recordKey := fmt.Sprintf("audit_record_%s", recordID)

		if err := s.client.Delete(ctx, recordKey); err != nil {
			return fmt.Errorf("failed to delete record %s: %w", recordID, err)
		}

		if err := s.removeFromIndex(ctx, recordID); err != nil {
			return fmt.Errorf("failed to remove record from index: %w", err)
		}
	}

	return nil
}

func (s *AuditLogStorageExtensionAdapter) GetAll(ctx context.Context) ([]Record, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	index, err := s.getIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get index: %w", err)
	}

	var records []Record
	for _, recordID := range index {
		recordKey := fmt.Sprintf("audit_record_%s", recordID)

		data, err := s.client.Get(ctx, recordKey)
		if err != nil {
			continue
		}

		record, err := s.deserializeRecord(data)
		if err != nil {
			continue
		}

		records = append(records, *record)
	}

	return records, nil
}

func (s *AuditLogStorageExtensionAdapter) generateRecordID(record *Record) string {
	if record == nil {
		return ""
	}

	bodyStr := ""
	if record.Body().Kind() != 0 {
		bodyStr = record.Body().String()
	}

	timestamp := record.Timestamp().UnixNano()
	severity := record.Severity().String()

	id := fmt.Sprintf("%d_%s_%s", timestamp, bodyStr, severity)

	return fmt.Sprintf("%d", hash(id))
}

func hash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func (s *AuditLogStorageExtensionAdapter) getIndex(ctx context.Context) ([]string, error) {
	data, err := s.client.Get(ctx, s.indexKey)
	if err != nil {
		return []string{}, nil
	}

	var index []string
	if err := json.Unmarshal(data, &index); err != nil {
		return []string{}, nil
	}

	return index, nil
}

func (s *AuditLogStorageExtensionAdapter) addToIndex(ctx context.Context, recordID string) error {
	index, err := s.getIndex(ctx)
	if err != nil {
		return err
	}

	for _, existingID := range index {
		if existingID == recordID {
			return nil
		}
	}

	index = append(index, recordID)

	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := s.client.Set(ctx, s.indexKey, data); err != nil {
		return fmt.Errorf("failed to save index: %w", err)
	}

	return nil
}

func (s *AuditLogStorageExtensionAdapter) removeFromIndex(ctx context.Context, recordID string) error {
	index, err := s.getIndex(ctx)
	if err != nil {
		return err
	}

	var newIndex []string
	for _, existingID := range index {
		if existingID != recordID {
			newIndex = append(newIndex, existingID)
		}
	}

	data, err := json.Marshal(newIndex)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := s.client.Set(ctx, s.indexKey, data); err != nil {
		return fmt.Errorf("failed to save index: %w", err)
	}

	return nil
}

func (s *AuditLogStorageExtensionAdapter) Close(ctx context.Context) error {
	return s.client.Close(ctx)
}

type SimpleKeyValueStorageClient struct {
	storage map[string][]byte
	mutex   sync.RWMutex
}

func NewSimpleKeyValueStorageClient() *SimpleKeyValueStorageClient {
	return &SimpleKeyValueStorageClient{
		storage: make(map[string][]byte),
	}
}

func (c *SimpleKeyValueStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.storage[key] = valueCopy

	return nil
}

func (c *SimpleKeyValueStorageClient) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.storage, key)
	return nil
}

func (c *SimpleKeyValueStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	for _, op := range ops {
		if err := op.Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *SimpleKeyValueStorageClient) Close(ctx context.Context) error {
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

type BoltDBStorageClient struct {
	filePath string
	mutex    sync.RWMutex
	storage  map[string][]byte
}

func NewBoltDBStorageClient(filePath string) (*BoltDBStorageClient, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}

	client := &BoltDBStorageClient{
		filePath: filePath,
		storage:  make(map[string][]byte),
	}

	if err := client.load(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to load storage: %w", err)
	}

	return client, nil
}

func (c *BoltDBStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
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

func (c *BoltDBStorageClient) Set(ctx context.Context, key string, value []byte) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.storage[key] = valueCopy

	return c.persist(ctx)
}

func (c *BoltDBStorageClient) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.storage, key)
	return c.persist(ctx)
}

func (c *BoltDBStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, op := range ops {
		switch o := op.(type) {
		case *SetOperation:
			valueCopy := make([]byte, len(o.Value))
			copy(valueCopy, o.Value)
			c.storage[o.Key] = valueCopy
		case *DeleteOperation:
			delete(c.storage, o.Key)
		}
	}

	return c.persist(ctx)
}

func (c *BoltDBStorageClient) Close(ctx context.Context) error {
	return c.persist(ctx)
}

func (c *BoltDBStorageClient) load(ctx context.Context) error {
	return nil
}

func (c *BoltDBStorageClient) persist(ctx context.Context) error {
	return nil
}

type RedisStorageClient struct {
	endpoint string
	password string
	db       int
	prefix   string

	expiration time.Duration

	mutex   sync.RWMutex
	storage map[string][]byte
}

type RedisStorageConfig struct {
	Endpoint   string
	Password   string
	DB         int
	Prefix     string
	Expiration time.Duration
}

func NewRedisStorageClient(config RedisStorageConfig) (*RedisStorageClient, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	client := &RedisStorageClient{
		endpoint:   config.Endpoint,
		password:   config.Password,
		db:         config.DB,
		prefix:     config.Prefix,
		expiration: config.Expiration,
		storage:    make(map[string][]byte),
	}

	return client, nil
}

func (c *RedisStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	fullKey := c.prefix + key
	value, exists := c.storage[fullKey]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

func (c *RedisStorageClient) Set(ctx context.Context, key string, value []byte) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	fullKey := c.prefix + key
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.storage[fullKey] = valueCopy

	return nil
}

func (c *RedisStorageClient) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	fullKey := c.prefix + key
	delete(c.storage, fullKey)
	return nil
}

func (c *RedisStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, op := range ops {
		switch o := op.(type) {
		case *SetOperation:
			fullKey := c.prefix + o.Key
			valueCopy := make([]byte, len(o.Value))
			copy(valueCopy, o.Value)
			c.storage[fullKey] = valueCopy
		case *DeleteOperation:
			fullKey := c.prefix + o.Key
			delete(c.storage, fullKey)
		}
	}

	return nil
}

func (c *RedisStorageClient) Close(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.storage = make(map[string][]byte)
	return nil
}

type SQLStorageClient struct {
	driver     string
	datasource string
	tableName  string

	mutex   sync.RWMutex
	storage map[string][]byte
}

type SQLStorageConfig struct {
	Driver     string
	Datasource string
	TableName  string
}

func NewSQLStorageClient(config SQLStorageConfig) (*SQLStorageClient, error) {
	if config.Driver == "" {
		return nil, fmt.Errorf("driver cannot be empty")
	}
	if config.Datasource == "" {
		return nil, fmt.Errorf("datasource cannot be empty")
	}

	tableName := config.TableName
	if tableName == "" {
		tableName = "audit_logs"
	}

	client := &SQLStorageClient{
		driver:     config.Driver,
		datasource: config.Datasource,
		tableName:  tableName,
		storage:    make(map[string][]byte),
	}

	if err := client.initialize(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize SQL storage: %w", err)
	}

	return client, nil
}

func (c *SQLStorageClient) initialize(ctx context.Context) error {
	return nil
}

func (c *SQLStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
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

func (c *SQLStorageClient) Set(ctx context.Context, key string, value []byte) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	c.storage[key] = valueCopy

	return nil
}

func (c *SQLStorageClient) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.storage, key)
	return nil
}

func (c *SQLStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, op := range ops {
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

func (c *SQLStorageClient) Close(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.storage = make(map[string][]byte)
	return nil
}
