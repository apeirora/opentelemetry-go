// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
)

type StorageExtension interface {
	GetClient(ctx context.Context, name string) (StorageClient, error)
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type StorageExtensionConfig interface {
	Validate() error
}

type MemoryStorageExtension struct {
	clients map[string]*SimpleKeyValueStorageClient
}

func NewMemoryStorageExtension() *MemoryStorageExtension {
	return &MemoryStorageExtension{
		clients: make(map[string]*SimpleKeyValueStorageClient),
	}
}

func (m *MemoryStorageExtension) Start(ctx context.Context) error {
	return nil
}

func (m *MemoryStorageExtension) Shutdown(ctx context.Context) error {
	for _, client := range m.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryStorageExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
	if client, exists := m.clients[name]; exists {
		return client, nil
	}

	client := NewSimpleKeyValueStorageClient()
	m.clients[name] = client
	return client, nil
}

type FileStorageConfig struct {
	Directory string
}

func (c *FileStorageConfig) Validate() error {
	if c.Directory == "" {
		return fmt.Errorf("directory cannot be empty")
	}
	return nil
}

type FileStorageExtension struct {
	config  *FileStorageConfig
	clients map[string]*BoltDBStorageClient
}

func NewFileStorageExtension(config *FileStorageConfig) (*FileStorageExtension, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &FileStorageExtension{
		config:  config,
		clients: make(map[string]*BoltDBStorageClient),
	}, nil
}

func (f *FileStorageExtension) Start(ctx context.Context) error {
	return nil
}

func (f *FileStorageExtension) Shutdown(ctx context.Context) error {
	for _, client := range f.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileStorageExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
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

type RedisStorageExtension struct {
	config  RedisStorageConfig
	clients map[string]*RealRedisStorageClient
}

func NewRedisStorageExtension(config RedisStorageConfig) (*RedisStorageExtension, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	return &RedisStorageExtension{
		config:  config,
		clients: make(map[string]*RealRedisStorageClient),
	}, nil
}

func (r *RedisStorageExtension) Start(ctx context.Context) error {
	return nil
}

func (r *RedisStorageExtension) Shutdown(ctx context.Context) error {
	for _, client := range r.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisStorageExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
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

type SQLStorageExtension struct {
	config  SQLStorageConfig
	clients map[string]*SQLStorageClient
}

func NewSQLStorageExtension(config SQLStorageConfig) (*SQLStorageExtension, error) {
	if config.Driver == "" {
		return nil, fmt.Errorf("driver cannot be empty")
	}
	if config.Datasource == "" {
		return nil, fmt.Errorf("datasource cannot be empty")
	}

	return &SQLStorageExtension{
		config:  config,
		clients: make(map[string]*SQLStorageClient),
	}, nil
}

func (s *SQLStorageExtension) Start(ctx context.Context) error {
	return nil
}

func (s *SQLStorageExtension) Shutdown(ctx context.Context) error {
	for _, client := range s.clients {
		if err := client.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStorageExtension) GetClient(ctx context.Context, name string) (StorageClient, error) {
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
