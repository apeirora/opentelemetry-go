// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/sdk/auditlog/recordcodec"
	"go.opentelemetry.io/otel/sdk/auditlog/storage"
)

type AuditLogStorageExtensionAdapter struct {
	client   storage.StorageClient
	mutex    sync.RWMutex
	indexKey string
}

func NewAuditLogStorageExtensionAdapter(client storage.StorageClient) (*AuditLogStorageExtensionAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("storage client cannot be nil")
	}
	return &AuditLogStorageExtensionAdapter{client: client, indexKey: "audit_log_index"}, nil
}

func (s *AuditLogStorageExtensionAdapter) Save(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	recordID, err := getAuditRecordID(record)
	if err != nil {
		return err
	}
	recordKey := fmt.Sprintf("audit_record_%s", recordID)
	recordHash := getAuditRecordHash(record)
	existingData, err := s.client.Get(ctx, recordKey)
	if err == nil {
		var decoded recordcodec.Data
		derr := json.Unmarshal(existingData, &decoded)
		if derr == nil {
			existingRecord := recordcodec.Deserialize(decoded)
			existingHash := getAuditRecordHash(&existingRecord)
			if existingHash != "" && recordHash != "" && existingHash != recordHash {
				return newAuditStatusError(AuditErrorConflict, "duplicate record_id with different hash", false, nil)
			}
			return nil
		}
	}
	payload, err := json.Marshal(recordcodec.Serialize(record))
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}
	if err := s.client.Set(ctx, recordKey, payload); err != nil {
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
		recordCopy := record
		recordID, err := getAuditRecordID(&recordCopy)
		if err != nil {
			continue
		}
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
		var decoded recordcodec.Data
		if err := json.Unmarshal(data, &decoded); err != nil {
			continue
		}
		record := recordcodec.Deserialize(decoded)
		records = append(records, record)
	}
	return records, nil
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
