// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/sdk/auditlog/identity"
	"go.opentelemetry.io/otel/sdk/auditlog/recordcodec"
	"go.opentelemetry.io/otel/sdk/auditlog/status"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type AuditLogStore interface {
	Save(ctx context.Context, record *sdklog.Record) error
	RemoveAll(ctx context.Context, records []sdklog.Record) error
	GetAll(ctx context.Context) ([]sdklog.Record, error)
}

type AuditLogFileStore struct {
	logFilePath string
	loggedRecords map[string]string
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
		loggedRecords: make(map[string]string),
		defaultLogFileName: DefaultLogFileName,
		logFileExtension: DefaultLogFileExtension,
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

func (s *AuditLogFileStore) LogFilePath() string {
	return s.logFilePath
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
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var data recordcodec.Data
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		record := recordcodec.Deserialize(data)
		recordID, err := identity.GetRecordID(&record)
		if err == nil {
			s.loggedRecords[recordID] = identity.GetRecordHash(&record)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan log file: %w", err)
	}
	return nil
}

func (s *AuditLogFileStore) Save(ctx context.Context, record *sdklog.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}
	recordID, err := identity.GetRecordID(record)
	if err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	recordHash := identity.GetRecordHash(record)
	if existingHash, exists := s.loggedRecords[recordID]; exists {
		if existingHash != "" && recordHash != "" && existingHash != recordHash {
			return status.New(status.ErrorConflict, "duplicate record_id with different hash", false, nil)
		}
		return nil
	}
	file, err := os.OpenFile(s.logFilePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file for writing: %w", err)
	}
	defer file.Close()
	data := recordcodec.Serialize(record)
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode record: %w", err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}
	s.loggedRecords[recordID] = recordHash
	return nil
}

func (s *AuditLogFileStore) RemoveAll(ctx context.Context, records []sdklog.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(records) == 0 {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	recordIDsToRemove := make(map[string]bool)
	for _, record := range records {
		recordID, err := identity.GetRecordID(&record)
		if err == nil {
			recordIDsToRemove[recordID] = true
		}
	}
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to open log file for reading: %w", err)
	}
	var remainingRecords []recordcodec.Data
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var data recordcodec.Data
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		record := recordcodec.Deserialize(data)
		recordID, err := identity.GetRecordID(&record)
		if err != nil {
			continue
		}
		if !recordIDsToRemove[recordID] {
			remainingRecords = append(remainingRecords, data)
		} else {
			delete(s.loggedRecords, recordID)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan source log file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close source log file: %w", err)
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
	if err := replaceFile(tempFile.Name(), s.logFilePath); err != nil {
		return fmt.Errorf("failed to replace original file: %w", err)
	}
	return nil
}

func replaceFile(sourcePath, targetPath string) error {
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	}
	return copyFileOverwrite(sourcePath, targetPath)
}

func copyFileOverwrite(sourcePath, targetPath string) error {
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	return errors.Join(copyErr, closeErr)
}

func (s *AuditLogFileStore) GetAll(ctx context.Context) ([]sdklog.Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()
	var records []sdklog.Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var data recordcodec.Data
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		record := recordcodec.Deserialize(data)
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan log file: %w", err)
	}
	return records, nil
}

func (s *AuditLogFileStore) WalkRecords(ctx context.Context, fn func(sdklog.Record) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var data recordcodec.Data
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		if err := fn(recordcodec.Deserialize(data)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan log file: %w", err)
	}
	return nil
}

func (s *AuditLogFileStore) PeekBatch(ctx context.Context, limit int) ([]sdklog.Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	file, err := os.Open(s.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()
	records := make([]sdklog.Record, 0, limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var data recordcodec.Data
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}
		records = append(records, recordcodec.Deserialize(data))
		if len(records) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan log file: %w", err)
	}
	return records, nil
}

type AuditLogInMemoryStore struct {
	records []sdklog.Record
	mutex   sync.RWMutex
}

func NewAuditLogInMemoryStore() *AuditLogInMemoryStore {
	return &AuditLogInMemoryStore{records: make([]sdklog.Record, 0)}
}

func (s *AuditLogInMemoryStore) Save(ctx context.Context, record *sdklog.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	recordCopy := record.Clone()
	s.records = append(s.records, recordCopy)
	return nil
}

func (s *AuditLogInMemoryStore) RemoveAll(ctx context.Context, records []sdklog.Record) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if len(records) == 0 {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	recordsToRemove := make(map[string]bool)
	for _, record := range records {
		recordCopy := record
		id, err := identity.GetRecordID(&recordCopy)
		if err == nil {
			recordsToRemove[id] = true
		}
	}
	var filteredRecords []sdklog.Record
	for _, record := range s.records {
		recordCopy := record
		id, err := identity.GetRecordID(&recordCopy)
		if err != nil || !recordsToRemove[id] {
			filteredRecords = append(filteredRecords, record)
		}
	}
	s.records = filteredRecords
	return nil
}

func (s *AuditLogInMemoryStore) GetAll(ctx context.Context) ([]sdklog.Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	records := make([]sdklog.Record, len(s.records))
	copy(records, s.records)
	return records, nil
}

func (s *AuditLogInMemoryStore) WalkRecords(ctx context.Context, fn func(sdklog.Record) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mutex.RLock()
	records := make([]sdklog.Record, len(s.records))
	copy(records, s.records)
	s.mutex.RUnlock()
	for _, record := range records {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := fn(record.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (s *AuditLogInMemoryStore) PeekBatch(ctx context.Context, limit int) ([]sdklog.Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if limit <= 0 {
		return nil, nil
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	n := limit
	if n > len(s.records) {
		n = len(s.records)
	}
	records := make([]sdklog.Record, n)
	copy(records, s.records[:n])
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
	s.records = make([]sdklog.Record, 0)
}
