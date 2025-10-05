// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type AuditLogStore interface {
	Save(ctx context.Context, record *Record) error
	RemoveAll(ctx context.Context, records []Record) error
	GetAll(ctx context.Context) ([]Record, error)
}

type AuditLogFileStore struct {
	logFilePath string

	loggedRecords map[string]bool

	mutex sync.RWMutex

	defaultLogFileName string

	logFileExtension string
}

const (
	DefaultLogFileExtension = ".log"

	DefaultLogFileName = "audit" + DefaultLogFileExtension
)

func NewAuditLogFileStore(path string) (*AuditLogFileStore, error) {
	store := &AuditLogFileStore{
		loggedRecords:      make(map[string]bool),
		defaultLogFileName: DefaultLogFileName,
		logFileExtension:   DefaultLogFileExtension,
	}

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		store.logFilePath = filepath.Join(path, DefaultLogFileName)
	} else {
		store.logFilePath = path
	}

	if err := os.MkdirAll(filepath.Dir(store.logFilePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	if _, err := os.Stat(store.logFilePath); os.IsNotExist(err) {
		file, err := os.Create(store.logFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create log file: %w", err)
		}
		file.Close()
	}

	if err := store.verifyFileAccess(); err != nil {
		return nil, err
	}

	if err := store.loadExistingRecordIds(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to load existing record IDs: %w", err)
	}

	return store, nil
}

func (s *AuditLogFileStore) verifyFileAccess() error {
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", s.logFilePath, err)
	}
	file.Close()

	file, err = os.OpenFile(s.logFilePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", s.logFilePath, err)
	}
	file.Close()

	return nil
}

func (s *AuditLogFileStore) generateRecordId(record *Record) string {
	if record == nil {
		return ""
	}

	bodyStr := ""
	if record.Body().Kind() != 0 {
		bodyStr = record.Body().String()
	}

	id := fmt.Sprintf("%d_%s_%s",
		record.Timestamp().UnixNano(),
		bodyStr,
		record.Severity().String(),
	)

	return fmt.Sprintf("%d", len(id))
}

func (s *AuditLogFileStore) loadExistingRecordIds(ctx context.Context) error {
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if info, err := file.Stat(); err != nil || info.Size() == 0 {
		return nil
	}

	decoder := json.NewDecoder(file)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var record Record
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		recordId := s.generateRecordId(&record)
		if recordId != "" {
			s.loggedRecords[recordId] = true
		}
	}

	return nil
}

func (s *AuditLogFileStore) Save(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	recordId := s.generateRecordId(record)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.loggedRecords[recordId] {
		return nil
	}

	file, err := os.OpenFile(s.logFilePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file for writing: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("failed to encode record: %w", err)
	}

	if _, err := file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	s.loggedRecords[recordId] = true

	return nil
}

func (s *AuditLogFileStore) RemoveAll(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	recordIdsToRemove := make(map[string]bool)
	for _, record := range records {
		recordId := s.generateRecordId(&record)
		if recordId != "" {
			recordIdsToRemove[recordId] = true
		}
	}

	file, err := os.Open(s.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to open log file for reading: %w", err)
	}
	defer file.Close()

	var remainingRecords []Record
	decoder := json.NewDecoder(file)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var record Record
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		recordId := s.generateRecordId(&record)
		if recordId != "" && !recordIdsToRemove[recordId] {
			remainingRecords = append(remainingRecords, record)
		} else {
			delete(s.loggedRecords, recordId)
		}
	}

	tempFile, err := os.CreateTemp(filepath.Dir(s.logFilePath), "audit_temp_*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	encoder := json.NewEncoder(tempFile)
	for _, record := range remainingRecords {
		if err := encoder.Encode(record); err != nil {
			tempFile.Close()
			return fmt.Errorf("failed to encode record: %w", err)
		}
		if _, err := tempFile.WriteString("\n"); err != nil {
			tempFile.Close()
			return fmt.Errorf("failed to write newline: %w", err)
		}
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tempFile.Name(), s.logFilePath); err != nil {
		return fmt.Errorf("failed to replace original file: %w", err)
	}

	return nil
}

func (s *AuditLogFileStore) GetAll(ctx context.Context) ([]Record, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	file, err := os.Open(s.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	var records []Record
	decoder := json.NewDecoder(file)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var record Record
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}
		records = append(records, record)
	}

	return records, nil
}

type AuditLogInMemoryStore struct {
	records []Record
	mutex   sync.RWMutex
}

func NewAuditLogInMemoryStore() *AuditLogInMemoryStore {
	return &AuditLogInMemoryStore{
		records: make([]Record, 0),
	}
}

func (s *AuditLogInMemoryStore) Save(ctx context.Context, record *Record) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	recordCopy := record.Clone()
	s.records = append(s.records, recordCopy)

	return nil
}

func (s *AuditLogInMemoryStore) RemoveAll(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	recordsToRemove := make(map[string]bool)
	for _, record := range records {
		bodyStr := ""
		if record.Body().Kind() != 0 {
			bodyStr = record.Body().String()
		}
		id := fmt.Sprintf("%d_%s", record.Timestamp().UnixNano(), bodyStr)
		recordsToRemove[id] = true
	}

	var filteredRecords []Record
	for _, record := range s.records {
		bodyStr := ""
		if record.Body().Kind() != 0 {
			bodyStr = record.Body().String()
		}
		id := fmt.Sprintf("%d_%s", record.Timestamp().UnixNano(), bodyStr)
		if !recordsToRemove[id] {
			filteredRecords = append(filteredRecords, record)
		}
	}

	s.records = filteredRecords
	return nil
}

func (s *AuditLogInMemoryStore) GetAll(ctx context.Context) ([]Record, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	records := make([]Record, len(s.records))
	copy(records, s.records)
	return records, nil
}

func (s *AuditLogInMemoryStore) GetRecordCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.records)
}

func (s *AuditLogInMemoryStore) Clear() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.records = make([]Record, 0)
}
