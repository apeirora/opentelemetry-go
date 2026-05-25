// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var validSQLTableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type SQLStorageClient struct {
	db        *sql.DB
	tableName string
	driver    string
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
	if !validSQLTableName.MatchString(tableName) {
		return nil, fmt.Errorf("invalid table name %q", tableName)
	}
	driver := strings.TrimSpace(config.Driver)
	db, err := sql.Open(driver, config.Datasource)
	if err != nil {
		return nil, fmt.Errorf("open sql database: %w", err)
	}
	client := &SQLStorageClient{
		db:        db,
		tableName: tableName,
		driver:    driver,
	}
	if err := client.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return client, nil
}

func (c *SQLStorageClient) initialize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sql database: %w", err)
	}
	ddl := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (key TEXT PRIMARY KEY, value BLOB NOT NULL)",
		c.tableName,
	)
	if _, err := c.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create table %s: %w", c.tableName, err)
	}
	return nil
}

func (c *SQLStorageClient) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT value FROM %s WHERE key = %s", c.tableName, c.placeholder(1))
	var value []byte
	err := c.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("get key %s: %w", key, err)
	}
	return value, nil
}

func (c *SQLStorageClient) Set(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	valueCopy := append([]byte(nil), value...)
	query, err := c.upsertQuery()
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, query, key, valueCopy)
	if err != nil {
		return fmt.Errorf("set key %s: %w", key, err)
	}
	return nil
}

func (c *SQLStorageClient) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE key = %s", c.tableName, c.placeholder(1))
	_, err := c.db.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("delete key %s: %w", key, err)
	}
	return nil
}

func (c *SQLStorageClient) Batch(ctx context.Context, ops ...Operation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin batch transaction: %w", err)
	}
	upsert, err := c.upsertQuery()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE key = %s", c.tableName, c.placeholder(1))
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			_ = tx.Rollback()
			return err
		}
		switch o := op.(type) {
		case *SetOperation:
			valueCopy := append([]byte(nil), o.Value...)
			if _, err := tx.ExecContext(ctx, upsert, o.Key, valueCopy); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("batch set %s: %w", o.Key, err)
			}
		case *DeleteOperation:
			if _, err := tx.ExecContext(ctx, deleteQuery, o.Key); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("batch delete %s: %w", o.Key, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}
	return nil
}

func (c *SQLStorageClient) Close(context.Context) error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *SQLStorageClient) placeholder(index int) string {
	switch c.driver {
	case "postgres", "pgx", "pgx/v5", "github.com/lib/pq":
		return fmt.Sprintf("$%d", index)
	default:
		return "?"
	}
}

func (c *SQLStorageClient) upsertQuery() (string, error) {
	switch c.driver {
	case "postgres", "pgx", "pgx/v5", "github.com/lib/pq":
		return fmt.Sprintf(
			"INSERT INTO %s (key, value) VALUES (%s, %s) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value",
			c.tableName, c.placeholder(1), c.placeholder(2),
		), nil
	case "mysql", "github.com/go-sql-driver/mysql":
		return fmt.Sprintf(
			"INSERT INTO %s (key, value) VALUES (%s, %s) ON DUPLICATE KEY UPDATE value = VALUES(value)",
			c.tableName, c.placeholder(1), c.placeholder(2),
		), nil
	case "sqlite", "sqlite3":
		return fmt.Sprintf(
			"INSERT INTO %s (key, value) VALUES (%s, %s) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			c.tableName, c.placeholder(1), c.placeholder(2),
		), nil
	default:
		return "", fmt.Errorf("unsupported sql driver %q for upsert (use sqlite, postgres, or mysql)", c.driver)
	}
}
