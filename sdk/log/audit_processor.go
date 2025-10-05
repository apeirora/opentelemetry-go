// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/log"
)

type AuditException struct {
	Message    string
	Cause      error
	Context    context.Context
	LogRecords []Record
}

func (e *AuditException) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *AuditException) Unwrap() error {
	return e.Cause
}

type AuditExceptionHandler interface {
	Handle(exception *AuditException)
}

type DefaultAuditExceptionHandler struct{}

func (h *DefaultAuditExceptionHandler) Handle(exception *AuditException) {
	fmt.Printf("AuditException: %s\n", exception.Error())
}

type RetryPolicy struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

func GetDefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       3,
		InitialBackoff:    time.Second,
		MaxBackoff:        time.Minute,
		BackoffMultiplier: 2.0,
	}
}

type AuditLogProcessorConfig struct {
	Exporter           Exporter
	AuditLogStore      AuditLogStore
	ExceptionHandler   AuditExceptionHandler
	ScheduleDelay      time.Duration
	MaxExportBatchSize int
	ExporterTimeout    time.Duration
	RetryPolicy        RetryPolicy
	WaitOnExport       bool
}

func DefaultAuditLogProcessorConfig(exporter Exporter, store AuditLogStore) AuditLogProcessorConfig {
	return AuditLogProcessorConfig{
		Exporter:           exporter,
		AuditLogStore:      store,
		ExceptionHandler:   &DefaultAuditExceptionHandler{},
		ScheduleDelay:      time.Second,
		MaxExportBatchSize: 512,
		ExporterTimeout:    30 * time.Second,
		RetryPolicy:        GetDefaultRetryPolicy(),
		WaitOnExport:       false,
	}
}

type PriorityRecord struct {
	Record   Record
	Priority int
}

type PriorityQueue []PriorityRecord

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].Priority > pq[j].Priority
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(PriorityRecord))
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

func getSeverityPriority(severity log.Severity) int {
	switch severity {
	case log.SeverityTrace, log.SeverityTrace2, log.SeverityTrace3, log.SeverityTrace4:
		return 1
	case log.SeverityDebug, log.SeverityDebug2, log.SeverityDebug3, log.SeverityDebug4:
		return 2
	case log.SeverityInfo, log.SeverityInfo2, log.SeverityInfo3, log.SeverityInfo4:
		return 3
	case log.SeverityWarn, log.SeverityWarn2, log.SeverityWarn3, log.SeverityWarn4:
		return 4
	case log.SeverityError, log.SeverityError2, log.SeverityError3, log.SeverityError4:
		return 5
	case log.SeverityFatal, log.SeverityFatal2, log.SeverityFatal3, log.SeverityFatal4:
		return 6
	default:
		return 0
	}
}

type AuditLogProcessor struct {
	config AuditLogProcessorConfig

	queue      *PriorityQueue
	queueMutex sync.Mutex

	shutdown atomic.Bool

	currentRetryAttempt atomic.Int32
	lastRetryTimestamp  atomic.Int64

	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewAuditLogProcessor(config AuditLogProcessorConfig) (*AuditLogProcessor, error) {
	if config.Exporter == nil {
		return nil, fmt.Errorf("exporter cannot be nil")
	}
	if config.AuditLogStore == nil {
		return nil, fmt.Errorf("audit log store cannot be nil")
	}

	processor := &AuditLogProcessor{
		config:   config,
		queue:    &PriorityQueue{},
		stopChan: make(chan struct{}),
	}

	heap.Init(processor.queue)

	if err := processor.loadExistingRecords(); err != nil {
		return nil, fmt.Errorf("failed to load existing records: %w", err)
	}

	processor.startBackgroundProcessing()

	return processor, nil
}

func (p *AuditLogProcessor) loadExistingRecords() error {
	records, err := p.config.AuditLogStore.GetAll(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get all records from store: %w", err)
	}

	p.queueMutex.Lock()
	defer p.queueMutex.Unlock()

	for _, record := range records {
		priority := getSeverityPriority(record.Severity())
		heap.Push(p.queue, PriorityRecord{
			Record:   record,
			Priority: priority,
		})
	}

	go p.exportLogs()

	return nil
}

func (p *AuditLogProcessor) startBackgroundProcessing() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.config.ScheduleDelay)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.exportLogs()
			case <-p.stopChan:
				return
			}
		}
	}()
}

func (p *AuditLogProcessor) OnEmit(ctx context.Context, record *Record) error {
	if record == nil {
		return nil
	}

	if p.shutdown.Load() {
		exception := &AuditException{
			Message:    "AuditLogProcessor is shutdown, cannot accept new logs",
			Context:    ctx,
			LogRecords: []Record{*record},
		}
		p.config.ExceptionHandler.Handle(exception)
		return exception
	}

	if err := p.config.AuditLogStore.Save(ctx, record); err != nil {
		exception := &AuditException{
			Message:    "Failed to save record to audit store",
			Cause:      err,
			Context:    ctx,
			LogRecords: []Record{*record},
		}
		p.config.ExceptionHandler.Handle(exception)
		return exception
	}

	p.queueMutex.Lock()
	priority := getSeverityPriority(record.Severity())
	heap.Push(p.queue, PriorityRecord{
		Record:   *record,
		Priority: priority,
	})
	queueSize := p.queue.Len()
	p.queueMutex.Unlock()

	if queueSize >= p.config.MaxExportBatchSize {
		go p.exportLogs()
	}

	return nil
}

func (p *AuditLogProcessor) exportLogs() {
	if p.shutdown.Load() {
		return
	}

	p.queueMutex.Lock()
	if p.queue.Len() == 0 {
		p.queueMutex.Unlock()
		return
	}
	p.queueMutex.Unlock()

	currentTime := time.Now().UnixMilli()
	if p.currentRetryAttempt.Load() > 0 {
		timeSinceLastRetry := currentTime - p.lastRetryTimestamp.Load()
		requiredDelay := p.calculateRetryDelay(int(p.currentRetryAttempt.Load()))

		if timeSinceLastRetry < requiredDelay {
			return
		}
	}

	var recordsToExport []Record
	p.queueMutex.Lock()
	batchSize := p.config.MaxExportBatchSize
	if batchSize > p.queue.Len() {
		batchSize = p.queue.Len()
	}

	for i := 0; i < batchSize && p.queue.Len() > 0; i++ {
		priorityRecord := heap.Pop(p.queue).(PriorityRecord)
		recordsToExport = append(recordsToExport, priorityRecord.Record)
	}
	p.queueMutex.Unlock()

	if len(recordsToExport) == 0 {
		return
	}

	ctx := context.Background()
	if p.config.ExporterTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.ExporterTimeout)
		defer cancel()
	}

	err := p.config.Exporter.Export(ctx, recordsToExport)
	if err != nil {
		p.handleExportFailure(recordsToExport, err)
	} else {
		p.currentRetryAttempt.Store(0)
		p.lastRetryTimestamp.Store(0)
		if err := p.config.AuditLogStore.RemoveAll(ctx, recordsToExport); err != nil {
			fmt.Printf("Failed to remove exported records from store: %v\n", err)
		}
	}
}

func (p *AuditLogProcessor) calculateRetryDelay(attemptNumber int) int64 {
	delay := float64(p.config.RetryPolicy.InitialBackoff.Milliseconds())
	delay *= float64(p.config.RetryPolicy.BackoffMultiplier * float64(attemptNumber-1))

	if delay > float64(p.config.RetryPolicy.MaxBackoff.Milliseconds()) {
		delay = float64(p.config.RetryPolicy.MaxBackoff.Milliseconds())
	}

	jitter := 0.25 * delay * (float64(time.Now().UnixNano()%1000)/1000.0 - 0.5)
	delay += jitter

	if delay < 0 {
		delay = 0
	}

	return int64(delay)
}

func (p *AuditLogProcessor) handleExportFailure(records []Record, cause error) {
	currentAttempt := p.currentRetryAttempt.Add(1)
	p.lastRetryTimestamp.Store(time.Now().UnixMilli())

	if currentAttempt <= int32(p.config.RetryPolicy.MaxAttempts) {
		p.queueMutex.Lock()
		for _, record := range records {
			priority := getSeverityPriority(record.Severity())
			heap.Push(p.queue, PriorityRecord{
				Record:   record,
				Priority: priority,
			})
		}
		p.queueMutex.Unlock()
		return
	}

	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)

	message := fmt.Sprintf("Export failed after %d retry attempts. Last error: %v",
		p.config.RetryPolicy.MaxAttempts, cause)

	exception := &AuditException{
		Message:    message,
		Cause:      cause,
		LogRecords: records,
	}

	p.config.ExceptionHandler.Handle(exception)
}

func (p *AuditLogProcessor) ForceFlush(ctx context.Context) error {
	if p.shutdown.Load() {
		return nil
	}

	for {
		p.queueMutex.Lock()
		queueLen := p.queue.Len()
		p.queueMutex.Unlock()

		if queueLen == 0 {
			break
		}

		p.exportLogs()

		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (p *AuditLogProcessor) Shutdown(ctx context.Context) error {
	if !p.shutdown.Swap(true) {
		close(p.stopChan)
		p.wg.Wait()

		return p.ForceFlush(ctx)
	}

	return nil
}

func (p *AuditLogProcessor) GetQueueSize() int {
	p.queueMutex.Lock()
	defer p.queueMutex.Unlock()
	return p.queue.Len()
}

func (p *AuditLogProcessor) GetRetryAttempts() int {
	return int(p.currentRetryAttempt.Load())
}
