// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import "go.opentelemetry.io/otel/sdk/auditlog/store"

type AuditLogStore = store.AuditLogStore
type AuditLogFileStore = store.AuditLogFileStore
type AuditLogInMemoryStore = store.AuditLogInMemoryStore

const (
	DefaultLogFileExtension = store.DefaultLogFileExtension
	DefaultLogFileName      = store.DefaultLogFileName
)

func NewAuditLogFileStore(path string) (*AuditLogFileStore, error) {
	return store.NewAuditLogFileStore(path)
}

func NewAuditLogInMemoryStore() *AuditLogInMemoryStore {
	return store.NewAuditLogInMemoryStore()
}
