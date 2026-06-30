// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"time"
)

type ExportCircuitState int32

const (
	ExportCircuitClosed ExportCircuitState = iota
	ExportCircuitOpen
	ExportCircuitHalfOpen
)

func (p *AuditLogProcessor) ExportCircuitState() ExportCircuitState {
	return ExportCircuitState(p.exportCircuitState.Load())
}

func (p *AuditLogProcessor) circuitOpenDuration() time.Duration {
	d := p.config.RetryPolicy.CircuitOpenDuration
	if d > 0 {
		return d
	}
	if p.config.RetryPolicy.MaxBackoff > 0 {
		return p.config.RetryPolicy.MaxBackoff
	}
	return time.Second
}

func (p *AuditLogProcessor) openExportCircuit() {
	p.circuitOpenUntil.Store(time.Now().Add(p.circuitOpenDuration()).UnixMilli())
	p.exportCircuitState.Store(int32(ExportCircuitOpen))
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
}

func (p *AuditLogProcessor) closeExportCircuit() {
	p.exportCircuitState.Store(int32(ExportCircuitClosed))
	p.circuitOpenUntil.Store(0)
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
}

func (p *AuditLogProcessor) exportCircuitIsOpen() bool {
	return p.exportCircuitState.Load() == int32(ExportCircuitOpen)
}

func (p *AuditLogProcessor) exportCircuitIsHalfOpen() bool {
	return p.exportCircuitState.Load() == int32(ExportCircuitHalfOpen)
}

func (p *AuditLogProcessor) maybeAdvanceExportCircuit() bool {
	if p.exportCircuitState.Load() != int32(ExportCircuitOpen) {
		return false
	}
	if time.Now().UnixMilli() < p.circuitOpenUntil.Load() {
		return false
	}
	if !p.exportCircuitState.CompareAndSwap(int32(ExportCircuitOpen), int32(ExportCircuitHalfOpen)) {
		return false
	}
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
	p.resyncStoreToQueue()
	p.scheduleExport()
	return true
}

func (p *AuditLogProcessor) resyncStoreToQueue() {
	type auditLogRecordWalker interface {
		WalkRecords(ctx context.Context, fn func(Record) error) error
	}

	var records []Record
	if walker, ok := p.config.AuditLogStore.(auditLogRecordWalker); ok {
		_ = walker.WalkRecords(context.Background(), func(record Record) error {
			records = append(records, record)
			return nil
		})
	} else {
		all, err := p.config.AuditLogStore.GetAll(context.Background())
		if err != nil {
			return
		}
		records = all
	}
	if len(records) == 0 {
		return
	}

	p.queueMutex.Lock()
	queuedIDs := make(map[string]struct{}, len(p.queue))
	for _, r := range p.queue {
		if id := recordIDFromSDKRecord(r); id != "" {
			queuedIDs[id] = struct{}{}
		}
	}
	added := 0
	for _, record := range records {
		id := recordIDFromSDKRecord(record)
		if id != "" {
			if _, exists := queuedIDs[id]; exists {
				continue
			}
			queuedIDs[id] = struct{}{}
		}
		p.queue = append(p.queue, record.Clone())
		added++
	}
	p.queueMutex.Unlock()
	if added > 0 {
		auditMetricsInstance().adjustQueueDepth(context.Background(), int64(added))
	}
}
