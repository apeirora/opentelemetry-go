// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"os"
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
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

func GetDefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
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
	DeliveryMode       AuditDeliveryMode
	StorageWriteMode   AuditStorageWriteMode
}

type AuditDeliveryMode string
type AuditStorageWriteMode string

const (
	AuditDeliveryModeAsyncStoreRetry AuditDeliveryMode = "async_store_retry"
	AuditDeliveryModeSyncDirect      AuditDeliveryMode = "sync_direct"

	AuditStorageWriteAlways  AuditStorageWriteMode = "always"
	AuditStorageWriteOnError AuditStorageWriteMode = "on_error"
)

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
		DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		StorageWriteMode:   AuditStorageWriteAlways,
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

	stopChan  chan struct{}
	wg        sync.WaitGroup
	extension StorageExtension
}

func auditReplayDebugEnabled() bool {
	v := os.Getenv("OTEL_AUDITLOG_DEBUG_REPLAY")
	return v == "1" || v == "true" || v == "TRUE"
}

func nonCancelContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (p *AuditLogProcessor) Enabled(ctx context.Context, param AuditEnabledParameters) bool {
	return !p.shutdown.Load()
}

func NewAuditLogProcessor(config AuditLogProcessorConfig) (*AuditLogProcessor, error) {
	if config.Exporter == nil {
		return nil, fmt.Errorf("exporter cannot be nil")
	}
	if config.ExceptionHandler == nil {
		config.ExceptionHandler = &DefaultAuditExceptionHandler{}
	}
	if config.DeliveryMode == "" {
		config.DeliveryMode = AuditDeliveryModeAsyncStoreRetry
	}
	if config.StorageWriteMode == "" {
		config.StorageWriteMode = AuditStorageWriteAlways
	}
	if config.DeliveryMode == AuditDeliveryModeAsyncStoreRetry && config.AuditLogStore == nil {
		return nil, fmt.Errorf("audit log store cannot be nil")
	}

	processor := &AuditLogProcessor{
		config:   config,
		queue:    &PriorityQueue{},
		stopChan: make(chan struct{}),
	}

	heap.Init(processor.queue)

	if config.DeliveryMode == AuditDeliveryModeAsyncStoreRetry {
		if err := processor.loadExistingRecords(); err != nil {
			return nil, fmt.Errorf("failed to load existing records: %w", err)
		}
		processor.startBackgroundProcessing()
	}

	return processor, nil
}

func (p *AuditLogProcessor) loadExistingRecords() error {
	type auditLogBatchPeeker interface {
		PeekBatch(ctx context.Context, limit int) ([]Record, error)
	}
	type auditLogRecordWalker interface {
		WalkRecords(ctx context.Context, fn func(Record) error) error
	}
	if peeker, ok := p.config.AuditLogStore.(auditLogBatchPeeker); ok {
		return p.loadExistingRecordsBatched(peeker)
	}
	if walker, ok := p.config.AuditLogStore.(auditLogRecordWalker); ok {
		return p.loadExistingRecordsStreaming(walker)
	}

	records, err := p.config.AuditLogStore.GetAll(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get all records from store: %w", err)
	}

	for _, record := range records {
		if err := p.enqueueLoadedRecord(record); err != nil {
			return err
		}
	}

	go func() {
		if err := p.exportLogs(false); err != nil {
			p.config.ExceptionHandler.Handle(&AuditException{
				Message:    "Failed to export loaded audit records from store",
				Cause:      err,
				Context:    context.Background(),
				LogRecords: nil,
			})
		}
	}()

	return nil
}

func (p *AuditLogProcessor) loadExistingRecordsBatched(peeker interface {
	PeekBatch(ctx context.Context, limit int) ([]Record, error)
}) error {
	batchSize := p.config.MaxExportBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	debugReplay := auditReplayDebugEnabled()
	if debugReplay {
		fmt.Printf("audit replay: start batch_size=%d\n", batchSize)
	}
	totalReplayed := 0
	for {
		records, err := peeker.PeekBatch(context.Background(), batchSize)
		if err != nil {
			if debugReplay {
				fmt.Printf("audit replay: peek error: %v\n", err)
			}
			return fmt.Errorf("peek stored records: %w", err)
		}
		if len(records) == 0 {
			if debugReplay {
				fmt.Printf("audit replay: store drained total_replayed=%d\n", totalReplayed)
			}
			break
		}
		if debugReplay {
			fmt.Printf("audit replay: fetched batch len=%d\n", len(records))
		}
		if err := p.replayStoredBatch(records); err != nil {
			if debugReplay {
				fmt.Printf("audit replay: batch failed len=%d err=%v (stopping replay loop)\n", len(records), err)
			}
			break
		}
		totalReplayed += len(records)
		if debugReplay {
			fmt.Printf("audit replay: batch delivered+removed len=%d total_replayed=%d\n", len(records), totalReplayed)
		}
	}
	go func() {
		if err := p.exportLogs(false); err != nil {
			p.config.ExceptionHandler.Handle(&AuditException{
				Message:    "Failed to export loaded audit records from store",
				Cause:      err,
				Context:    context.Background(),
				LogRecords: nil,
			})
		}
	}()
	return nil
}

func (p *AuditLogProcessor) loadExistingRecordsStreaming(walker interface {
	WalkRecords(ctx context.Context, fn func(Record) error) error
}) error {
	batchSize := p.config.MaxExportBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	batch := make([]Record, 0, batchSize)
	stopReplay := false
	if err := walker.WalkRecords(context.Background(), func(record Record) error {
		if stopReplay {
			return nil
		}
		batch = append(batch, record)
		if len(batch) < batchSize {
			return nil
		}
		if err := p.replayStoredBatch(batch); err != nil {
			stopReplay = true
			return nil
		}
		batch = batch[:0]
		return nil
	}); err != nil {
		return fmt.Errorf("walk stored records: %w", err)
	}
	if !stopReplay && len(batch) > 0 {
		if err := p.replayStoredBatch(batch); err != nil {
			stopReplay = true
		}
	}
	go func() {
		if err := p.exportLogs(false); err != nil {
			p.config.ExceptionHandler.Handle(&AuditException{
				Message:    "Failed to export loaded audit records from store",
				Cause:      err,
				Context:    context.Background(),
				LogRecords: nil,
			})
		}
	}()
	return nil
}

func (p *AuditLogProcessor) enqueueLoadedRecord(record Record) error {
	p.queueMutex.Lock()
	priority := getSeverityPriority(record.Severity())
	heap.Push(p.queue, PriorityRecord{
		Record:   record,
		Priority: priority,
	})
	p.queueMutex.Unlock()
	return nil
}

func (p *AuditLogProcessor) replayStoredBatch(records []Record) error {
	ctx := context.Background()
	if p.config.ExporterTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.ExporterTimeout)
		defer cancel()
	}
	err := p.config.Exporter.Export(ctx, records)
	if err != nil {
		p.handleExportFailure(records, err)
		return err
	}
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
	return p.removeExportedRecordsFromStore(ctx, records)
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
				if err := p.exportLogs(false); err != nil {
					p.config.ExceptionHandler.Handle(&AuditException{
						Message:    "Background audit export failed",
						Cause:      err,
						Context:    context.Background(),
						LogRecords: nil,
					})
				}
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

	if p.config.DeliveryMode == AuditDeliveryModeSyncDirect {
		exportCtx := nonCancelContext(ctx)
		if p.config.ExporterTimeout > 0 {
			var cancel context.CancelFunc
			exportCtx, cancel = context.WithTimeout(exportCtx, p.config.ExporterTimeout)
			defer cancel()
		}
		if err := p.config.Exporter.Export(exportCtx, []Record{*record}); err != nil {
			exception := &AuditException{
				Message:    "Failed to export record directly",
				Cause:      err,
				Context:    ctx,
				LogRecords: []Record{*record},
			}
			p.config.ExceptionHandler.Handle(exception)
			return exception
		}
		return nil
	}

	if p.config.StorageWriteMode == AuditStorageWriteAlways {
		storeCtx := nonCancelContext(ctx)
		if err := p.config.AuditLogStore.Save(storeCtx, record); err != nil {
			exception := &AuditException{
				Message:    "Failed to save record to audit store",
				Cause:      err,
				Context:    ctx,
				LogRecords: []Record{*record},
			}
			p.config.ExceptionHandler.Handle(exception)
			return exception
		}
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
		go func() {
			if err := p.exportLogs(false); err != nil {
				p.config.ExceptionHandler.Handle(&AuditException{
					Message:    "Audit export failed after queue reached batch size",
					Cause:      err,
					Context:    ctx,
					LogRecords: nil,
				})
			}
		}()
	}

	return nil
}

func (p *AuditLogProcessor) exportLogs(ignoreRetryDelay bool) error {
	if p.shutdown.Load() && !ignoreRetryDelay {
		return nil
	}

	p.queueMutex.Lock()
	if p.queue.Len() == 0 {
		p.queueMutex.Unlock()
		return nil
	}
	p.queueMutex.Unlock()

	currentTime := time.Now().UnixMilli()
	if !ignoreRetryDelay && p.currentRetryAttempt.Load() > 0 {
		timeSinceLastRetry := currentTime - p.lastRetryTimestamp.Load()
		requiredDelay := p.calculateRetryDelay(int(p.currentRetryAttempt.Load()))

		if timeSinceLastRetry < requiredDelay {
			return nil
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
		return nil
	}

	ctx := context.Background()
	if p.config.ExporterTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.ExporterTimeout)
		defer cancel()
	}

	shouldRemove := p.shouldRemoveExportedRecordsFromStore()
	err := p.config.Exporter.Export(ctx, recordsToExport)
	if err != nil {
		p.handleExportFailure(recordsToExport, err)
		return err
	}
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
	if !shouldRemove {
		return nil
	}
	return p.removeExportedRecordsFromStore(ctx, recordsToExport)
}

func (p *AuditLogProcessor) shouldRemoveExportedRecordsFromStore() bool {
	if p.config.StorageWriteMode == AuditStorageWriteAlways {
		return true
	}
	return p.currentRetryAttempt.Load() > 0
}

func (p *AuditLogProcessor) removeExportedRecordsFromStore(ctx context.Context, records []Record) error {
	var removeErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		removeErr = p.config.AuditLogStore.RemoveAll(ctx, records)
		if removeErr == nil {
			return nil
		}
	}
	p.config.ExceptionHandler.Handle(&AuditException{
		Message:    "Exported audit records but failed to remove them from the store (telemetry may have been delivered; file may still show old entries)",
		Cause:      removeErr,
		Context:    ctx,
		LogRecords: records,
	})
	return fmt.Errorf("remove exported records from store: %w", removeErr)
}

func (p *AuditLogProcessor) calculateRetryDelay(attemptNumber int) int64 {
	if attemptNumber < 1 {
		attemptNumber = 1
	}
	delay := float64(p.config.RetryPolicy.InitialBackoff.Milliseconds())
	delay *= math.Pow(p.config.RetryPolicy.BackoffMultiplier, float64(attemptNumber-1))

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
	if p.config.StorageWriteMode == AuditStorageWriteOnError {
		storeCtx := context.Background()
		for _, record := range records {
			recordCopy := record
			if err := p.config.AuditLogStore.Save(storeCtx, &recordCopy); err != nil {
				p.config.ExceptionHandler.Handle(&AuditException{
					Message:    "Failed to save failed export record to audit store",
					Cause:      err,
					Context:    storeCtx,
					LogRecords: []Record{recordCopy},
				})
			}
		}
	}
	p.currentRetryAttempt.Add(1)
	p.lastRetryTimestamp.Store(time.Now().UnixMilli())

	exception := &AuditException{
		Message:    "Failed to export audit log records",
		Cause:      cause,
		Context:    context.Background(),
		LogRecords: records,
	}
	p.config.ExceptionHandler.Handle(exception)

	p.queueMutex.Lock()
	for _, record := range records {
		priority := getSeverityPriority(record.Severity())
		heap.Push(p.queue, PriorityRecord{
			Record:   record,
			Priority: priority,
		})
	}
	p.queueMutex.Unlock()
}

func (p *AuditLogProcessor) ForceFlush(ctx context.Context) error {
	if p.config.DeliveryMode == AuditDeliveryModeSyncDirect {
		return nil
	}

	for {
		p.queueMutex.Lock()
		queueLen := p.queue.Len()
		p.queueMutex.Unlock()

		if queueLen == 0 {
			break
		}

		retryBefore := p.currentRetryAttempt.Load()
		exportErr := p.exportLogs(true)
		retryAfter := p.currentRetryAttempt.Load()

		p.queueMutex.Lock()
		queueAfter := p.queue.Len()
		p.queueMutex.Unlock()

		if exportErr != nil {
			if queueAfter >= queueLen && retryAfter > retryBefore {
				return fmt.Errorf("failed to flush audit queue: export attempts are failing")
			}
			return exportErr
		}

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

		err := p.ForceFlush(ctx)
		if shutdownErr := p.config.Exporter.Shutdown(ctx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}

		if p.extension != nil {
			if shutdownErr := p.extension.Shutdown(ctx); shutdownErr != nil && err == nil {
				err = shutdownErr
			}
		}

		return err
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
