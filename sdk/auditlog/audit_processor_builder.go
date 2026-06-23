// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"fmt"
	"time"
)

type AuditLogProcessorBuilder struct {
	config                 AuditLogProcessorConfig
	storageConfig          *storageConfig
	extension              StorageExtension
	verifyExporterOnStartup bool
	startupVerifyTimeout    time.Duration
}

func NewAuditLogProcessorBuilder(exporter Exporter, store AuditLogStore) (*AuditLogProcessorBuilder, error) {
	if exporter == nil {
		return nil, fmt.Errorf("exporter cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
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
			WaitOnExport:       true,
		},
		verifyExporterOnStartup: true,
		startupVerifyTimeout:    10 * time.Second,
	}, nil
}

func NewAuditLogProcessorWithStorage(exporter Exporter) (*AuditLogProcessorBuilder, error) {
	if exporter == nil {
		return nil, fmt.Errorf("exporter cannot be nil")
	}

	return &AuditLogProcessorBuilder{
		config: AuditLogProcessorConfig{
			Exporter:           exporter,
			ExceptionHandler:   &DefaultAuditExceptionHandler{},
			ScheduleDelay:      time.Second,
			MaxExportBatchSize: 512,
			ExporterTimeout:    30 * time.Second,
			RetryPolicy:        GetDefaultRetryPolicy(),
			WaitOnExport:       true,
		},
		verifyExporterOnStartup: true,
		startupVerifyTimeout:    10 * time.Second,
	}, nil
}

func (b *AuditLogProcessorBuilder) SetExceptionHandler(handler AuditExceptionHandler) *AuditLogProcessorBuilder {
	if handler != nil {
		b.config.ExceptionHandler = handler
	}
	return b
}

func (b *AuditLogProcessorBuilder) SetScheduleDelay(delay time.Duration) *AuditLogProcessorBuilder {
	b.config.ScheduleDelay = delay
	return b
}

func (b *AuditLogProcessorBuilder) SetMaxExportBatchSize(size int) *AuditLogProcessorBuilder {
	b.config.MaxExportBatchSize = size
	return b
}

func (b *AuditLogProcessorBuilder) SetExporterTimeout(timeout time.Duration) *AuditLogProcessorBuilder {
	b.config.ExporterTimeout = timeout
	return b
}

func (b *AuditLogProcessorBuilder) SetRetryPolicy(policy RetryPolicy) *AuditLogProcessorBuilder {
	b.config.RetryPolicy = policy
	return b
}

func (b *AuditLogProcessorBuilder) SetWaitOnExport(wait bool) *AuditLogProcessorBuilder {
	b.config.WaitOnExport = wait
	return b
}

func (b *AuditLogProcessorBuilder) SetVerifyExporterOnStartup(verify bool) *AuditLogProcessorBuilder {
	b.verifyExporterOnStartup = verify
	return b
}

func (b *AuditLogProcessorBuilder) SetStartupVerifyTimeout(timeout time.Duration) *AuditLogProcessorBuilder {
	b.startupVerifyTimeout = timeout
	return b
}

func (b *AuditLogProcessorBuilder) Build() (*AuditLogProcessor, error) {
	if err := b.ValidateConfig(); err != nil {
		return nil, err
	}

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

	if b.verifyExporterOnStartup {
		if verifier, ok := b.config.Exporter.(StartupExporterVerifier); ok {
			timeout := b.startupVerifyTimeout
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			if b.config.ExporterTimeout > 0 && b.config.ExporterTimeout < timeout {
				timeout = b.config.ExporterTimeout
			}
			vctx, cancel := context.WithTimeout(ctx, timeout)
			err := verifier.VerifyStartup(vctx)
			cancel()
			if err != nil {
				if b.extension != nil {
					b.extension.Shutdown(ctx)
				}
				return nil, fmt.Errorf("exporter startup verification failed: %w", err)
			}
		}
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

func (b *AuditLogProcessorBuilder) GetConfig() AuditLogProcessorConfig {
	return b.config
}

func (b *AuditLogProcessorBuilder) ValidateConfig() error {
	if b.config.Exporter == nil {
		return fmt.Errorf("exporter is required")
	}
	if b.config.AuditLogStore == nil && b.storageConfig == nil {
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
	if b.config.RetryPolicy.MaxAttempts < 0 {
		return fmt.Errorf("retry policy max attempts must be non-negative")
	}
	return nil
}
