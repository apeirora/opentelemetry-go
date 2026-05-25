// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const defaultBoltBucket = "auditlog"

type BoltDBStorageClient struct {
	db   *bolt.DB
	path string
	mu   sync.RWMutex
}

func NewBoltDBStorageClient(filePath string) (*BoltDBStorageClient, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}
	dir := filepath.Dir(filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("create bolt directory: %w", err)
		}
	}
	db, err := bolt.Open(filePath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bolt database %q: %w", filePath, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(defaultBoltBucket))
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize bolt bucket: %w", err)
	}
	return &BoltDBStorageClient{db: db, path: filePath}, nil
}

func (c *BoltDBStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return nil, fmt.Errorf("bolt database is closed")
	}
	var value []byte
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(defaultBoltBucket))
		if b == nil {
			return fmt.Errorf("key not found: %s", key)
		}
		v := b.Get([]byte(key))
		if v == nil {
			return fmt.Errorf("key not found: %s", key)
		}
		value = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (c *BoltDBStorageClient) Set(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return fmt.Errorf("bolt database is closed")
	}
	valueCopy := append([]byte(nil), value...)
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(defaultBoltBucket))
		if b == nil {
			return fmt.Errorf("bolt bucket missing")
		}
		return b.Put([]byte(key), valueCopy)
	})
}

func (c *BoltDBStorageClient) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return fmt.Errorf("bolt database is closed")
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(defaultBoltBucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

func (c *BoltDBStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return fmt.Errorf("bolt database is closed")
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(defaultBoltBucket))
		if b == nil {
			return fmt.Errorf("bolt bucket missing")
		}
		for _, op := range ops {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch o := op.(type) {
			case *SetOperation:
				valueCopy := append([]byte(nil), o.Value...)
				if err := b.Put([]byte(o.Key), valueCopy); err != nil {
					return err
				}
			case *DeleteOperation:
				if err := b.Delete([]byte(o.Key)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (c *BoltDBStorageClient) Close(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	return err
}
