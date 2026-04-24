// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"fmt"
)

type Extension interface {
	GetClient(ctx context.Context, name string) (StorageClient, error)
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type ExtensionConfig interface {
	Validate() error
}

type MemoryExtension struct {
	clients map[string]*SimpleKeyValueStorageClient
}

func NewMemoryExtension() *MemoryExtension {
	return &MemoryExtension{clients: make(map[string]*SimpleKeyValueStorageClient)}
}

func (m *MemoryExtension) Start(ctx context.Context) error { return nil }

func (m *MemoryExtension) Shutdown(ctx context.Context) error {
	for _, client := range m.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
	if client, exists := m.clients[name]; exists {
		return client, nil
	}
	client := NewSimpleKeyValueStorageClient()
	m.clients[name] = client
	return client, nil
}

type FileConfig struct {
	Directory string
}

func (c *FileConfig) Validate() error {
	if c.Directory == "" {
		return fmt.Errorf("directory cannot be empty")
	}
	return nil
}

type FileExtension struct {
	config  *FileConfig
	clients map[string]*BoltDBStorageClient
}

func NewFileExtension(config *FileConfig) (*FileExtension, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &FileExtension{config: config, clients: make(map[string]*BoltDBStorageClient)}, nil
}

func (f *FileExtension) Start(ctx context.Context) error { return nil }

func (f *FileExtension) Shutdown(ctx context.Context) error {
	for _, client := range f.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
	if client, exists := f.clients[name]; exists {
		return client, nil
	}
	filePath := fmt.Sprintf("%s/%s.db", f.config.Directory, name)
	client, err := NewBoltDBStorageClient(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file storage client: %w", err)
	}
	f.clients[name] = client
	return client, nil
}

type RedisExtension struct {
	config  RedisStorageConfig
	clients map[string]*RealRedisStorageClient
}

func NewRedisExtension(config RedisStorageConfig) (*RedisExtension, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}
	return &RedisExtension{config: config, clients: make(map[string]*RealRedisStorageClient)}, nil
}

func (r *RedisExtension) Start(ctx context.Context) error { return nil }

func (r *RedisExtension) Shutdown(ctx context.Context) error {
	for _, client := range r.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
	if client, exists := r.clients[name]; exists {
		return client, nil
	}
	clientConfig := r.config
	clientConfig.Prefix = r.config.Prefix + name + "_"
	client, err := NewRealRedisStorageClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis storage client: %w", err)
	}
	r.clients[name] = client
	return client, nil
}

type SQLExtension struct {
	config  SQLStorageConfig
	clients map[string]*SQLStorageClient
}

func NewSQLExtension(config SQLStorageConfig) (*SQLExtension, error) {
	if config.Driver == "" {
		return nil, fmt.Errorf("driver cannot be empty")
	}
	if config.Datasource == "" {
		return nil, fmt.Errorf("datasource cannot be empty")
	}
	return &SQLExtension{config: config, clients: make(map[string]*SQLStorageClient)}, nil
}

func (s *SQLExtension) Start(ctx context.Context) error { return nil }

func (s *SQLExtension) Shutdown(ctx context.Context) error {
	for _, client := range s.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
	if client, exists := s.clients[name]; exists {
		return client, nil
	}
	clientConfig := s.config
	clientConfig.TableName = s.config.TableName + "_" + name
	client, err := NewSQLStorageClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create SQL storage client: %w", err)
	}
	s.clients[name] = client
	return client, nil
}
