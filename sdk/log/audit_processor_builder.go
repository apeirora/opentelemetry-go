// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"context"
	"fmt"
	"time"
)

type AuditLogProcessorBuilder struct {
	config        AuditLogProcessorConfig
	storageConfig *storageConfig
	extension     StorageExtension
}

func NewAuditLogProcessorBuilder(exporter Exporter, store AuditLogStore) *AuditLogProcessorBuilder {
	if exporter == nil {
		panic("exporter cannot be nil")
	}
	if store == nil {
		panic("store cannot be nil")
	}

	return &AuditLogProcessorBuilder{
		config: AuditLogProcessorConfig{
			Exporter:           exporter,
			AuditLogStore:      store,
			ExceptionHandler:   &DefaultAuditExceptionHandler{},
			ScheduleDelay:      time.Second,
			MaxExportBatchSize: 512,
			ExporterTimeout:    30 * time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
		},
	}
}

func NewAuditLogProcessorWithStorage(exporter Exporter) *AuditLogProcessorBuilder {
	if exporter == nil {
		panic("exporter cannot be nil")
	}

	return &AuditLogProcessorBuilder{
		config: AuditLogProcessorConfig{
			Exporter:           exporter,
			ExceptionHandler:   &DefaultAuditExceptionHandler{},
			ScheduleDelay:      time.Second,
			MaxExportBatchSize: 512,
			ExporterTimeout:    30 * time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       false,
		},
	}
}

func (b *AuditLogProcessorBuilder) SetExceptionHandler(handler AuditExceptionHandler) *AuditLogProcessorBuilder {
	if handler == nil {
		panic("exception handler cannot be nil")
	}
	b.config.ExceptionHandler = handler
	return b
}

func (b *AuditLogProcessorBuilder) SetScheduleDelay(delay time.Duration) *AuditLogProcessorBuilder {
	if delay < 0 {
		panic("schedule delay must be non-negative")
	}
	b.config.ScheduleDelay = delay
	return b
}

func (b *AuditLogProcessorBuilder) SetMaxExportBatchSize(size int) *AuditLogProcessorBuilder {
	if size <= 0 {
		panic("max export batch size must be positive")
	}
	b.config.MaxExportBatchSize = size
	return b
}

func (b *AuditLogProcessorBuilder) SetExporterTimeout(timeout time.Duration) *AuditLogProcessorBuilder {
	if timeout < 0 {
		panic("exporter timeout must be non-negative")
	}
	b.config.ExporterTimeout = timeout
	return b
}

func (b *AuditLogProcessorBuilder) SetRetryPolicy(policy RetryPolicy) *AuditLogProcessorBuilder {
	if policy.InitialBackoff < 0 {
		panic("retry policy initial backoff must be non-negative")
	}
	if policy.MaxBackoff < 0 {
		panic("retry policy max backoff must be non-negative")
	}
	if policy.BackoffMultiplier <= 0 {
		panic("retry policy backoff multiplier must be positive")
	}
	b.config.RetryPolicy = policy
	return b
}

func (b *AuditLogProcessorBuilder) SetWaitOnExport(wait bool) *AuditLogProcessorBuilder {
	b.config.WaitOnExport = wait
	return b
}

func (b *AuditLogProcessorBuilder) Build() (*AuditLogProcessor, error) {
	ctx := context.Background()

	if b.config.AuditLogStore == nil && b.storageConfig != nil {
		extension, err := b.createStorageExtension()
		if err != nil {
			return nil, fmt.Errorf("failed to create storage extension: %w", err)
		}

		if err := extension.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start storage extension: %w", err)
		}

		clientName := "audit_processor"
		if b.storageConfig.clientName != "" {
			clientName = b.storageConfig.clientName
		}

		client, err := extension.GetClient(ctx, clientName)
		if err != nil {
			extension.Shutdown(ctx)
			return nil, fmt.Errorf("failed to get storage client: %w", err)
		}

		adapter, err := NewAuditLogStorageExtensionAdapter(client)
		if err != nil {
			extension.Shutdown(ctx)
			return nil, fmt.Errorf("failed to create storage adapter: %w", err)
		}

		b.config.AuditLogStore = adapter
		b.extension = extension
	}

	processor, err := NewAuditLogProcessor(b.config)
	if err != nil {
		if b.extension != nil {
			b.extension.Shutdown(ctx)
		}
		return nil, fmt.Errorf("failed to create audit log processor: %w", err)
	}

	processor.extension = b.extension

	return processor, nil
}

func (b *AuditLogProcessorBuilder) BuildOrPanic() *AuditLogProcessor {
	processor, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to create audit log processor: %v", err))
	}
	return processor
}

func (b *AuditLogProcessorBuilder) GetConfig() AuditLogProcessorConfig {
	return b.config
}

func (b *AuditLogProcessorBuilder) ValidateConfig() error {
	if b.config.Exporter == nil {
		return fmt.Errorf("exporter is required")
	}
	if b.config.AuditLogStore == nil {
		return fmt.Errorf("audit log store is required")
	}
	if b.config.ExceptionHandler == nil {
		return fmt.Errorf("exception handler is required")
	}
	if b.config.ScheduleDelay < 0 {
		return fmt.Errorf("schedule delay must be non-negative")
	}
	if b.config.MaxExportBatchSize <= 0 {
		return fmt.Errorf("max export batch size must be positive")
	}
	if b.config.ExporterTimeout < 0 {
		return fmt.Errorf("exporter timeout must be non-negative")
	}
	if b.config.RetryPolicy.InitialBackoff < 0 {
		return fmt.Errorf("retry policy initial backoff must be non-negative")
	}
	if b.config.RetryPolicy.MaxBackoff < 0 {
		return fmt.Errorf("retry policy max backoff must be non-negative")
	}
	if b.config.RetryPolicy.BackoffMultiplier <= 0 {
		return fmt.Errorf("retry policy backoff multiplier must be positive")
	}
	return nil
}
