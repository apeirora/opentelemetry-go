// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/audit"
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
	otel.Handle(exception)
}

type RetryPolicy struct {
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	// MaxAttempts limits export retry cycles after a failed batch. Zero means unlimited.
	MaxAttempts int
}

func GetDefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		InitialBackoff:    time.Second,
		MaxBackoff:        time.Minute,
		BackoffMultiplier: 2.0,
		MaxAttempts:       0,
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

	// AuditStorageWriteAlways persists each record to the configured store before it enters
	// the in-memory export queue, so crashes can recover from disk/Redis/SQL on restart.
	AuditStorageWriteAlways AuditStorageWriteMode = "always"
	// AuditStorageWriteOnError persists only after an export failure. Records accepted while
	// the process is healthy live only in the in-memory queue until export succeeds or fails.
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
		WaitOnExport:       true,
		DeliveryMode:       AuditDeliveryModeAsyncStoreRetry,
		StorageWriteMode:   AuditStorageWriteAlways,
	}
}

type AuditLogProcessor struct {
	config AuditLogProcessorConfig

	queue      []Record
	queueMutex sync.Mutex

	shutdown atomic.Bool

	currentRetryAttempt atomic.Int32
	lastRetryTimestamp  atomic.Int64

	stopChan   chan struct{}
	wakeExport chan struct{}
	wg         sync.WaitGroup
	extension  StorageExtension

	flushReceiptsMu sync.Mutex
	flushReceipts   map[string]auditReceiptEntry

	droppedRecordsMu sync.Mutex
	droppedRecords   map[string]error
}

type auditReceiptEntry struct {
	receipt audit.AuditReceipt
}

func (p *AuditLogProcessor) storeFlushReceipts(receipts []audit.AuditReceipt) {
	if len(receipts) == 0 {
		return
	}
	p.flushReceiptsMu.Lock()
	defer p.flushReceiptsMu.Unlock()
	if p.flushReceipts == nil {
		p.flushReceipts = make(map[string]auditReceiptEntry, len(receipts))
	}
	for _, r := range receipts {
		if r.RecordID == "" {
			continue
		}
		p.flushReceipts[r.RecordID] = auditReceiptEntry{receipt: r}
	}
}

func (p *AuditLogProcessor) markRecordsDropped(records []Record, cause error) {
	p.droppedRecordsMu.Lock()
	defer p.droppedRecordsMu.Unlock()
	if p.droppedRecords == nil {
		p.droppedRecords = make(map[string]error)
	}
	dropErr := fmt.Errorf("audit record dropped after retry exhaustion: %w", cause)
	for _, rec := range records {
		id := recordIDFromSDKRecord(rec)
		if id != "" {
			p.droppedRecords[id] = dropErr
		}
	}
}

func (p *AuditLogProcessor) takeDroppedError(recordID string) error {
	p.droppedRecordsMu.Lock()
	defer p.droppedRecordsMu.Unlock()
	if p.droppedRecords == nil {
		return nil
	}
	err, ok := p.droppedRecords[recordID]
	if !ok {
		return nil
	}
	delete(p.droppedRecords, recordID)
	return err
}

func (p *AuditLogProcessor) ReceiptFor(recordID string) (audit.AuditReceipt, bool) {
	p.flushReceiptsMu.Lock()
	defer p.flushReceiptsMu.Unlock()
	if p.flushReceipts == nil {
		return audit.AuditReceipt{}, false
	}
	e, ok := p.flushReceipts[recordID]
	if !ok {
		return audit.AuditReceipt{}, false
	}
	delete(p.flushReceipts, recordID)
	return e.receipt, true
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
		stopChan: make(chan struct{}),
	}

	if config.DeliveryMode == AuditDeliveryModeAsyncStoreRetry {
		processor.startBackgroundProcessing()
		if err := processor.loadExistingRecords(); err != nil {
			processor.shutdown.Store(true)
			close(processor.stopChan)
			processor.wg.Wait()
			return nil, fmt.Errorf("failed to load existing records: %w", err)
		}
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

	p.scheduleExport()

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
	p.scheduleExport()
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
	p.scheduleExport()
	return nil
}

func (p *AuditLogProcessor) scheduleExport() {
	if p.shutdown.Load() || p.wakeExport == nil {
		return
	}
	select {
	case p.wakeExport <- struct{}{}:
	default:
	}
}

func (p *AuditLogProcessor) invokeExport(message string) {
	if err := p.exportLogs(false); err != nil {
		p.config.ExceptionHandler.Handle(&AuditException{
			Message:    message,
			Cause:      err,
			Context:    context.Background(),
			LogRecords: nil,
		})
	}
}

func (p *AuditLogProcessor) enqueueLoadedRecord(record Record) error {
	p.queueMutex.Lock()
	p.queue = append(p.queue, record.Clone())
	p.queueMutex.Unlock()
	auditMetricsInstance().adjustQueueDepth(context.Background(), 1)
	return nil
}

func (p *AuditLogProcessor) replayStoredBatch(records []Record) error {
	ctx := context.Background()
	if p.config.ExporterTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.ExporterTimeout)
		defer cancel()
	}
	exportStart := time.Now()
	exportResult, err := p.config.Exporter.Export(ctx, records)
	auditMetricsInstance().recordExportDuration(ctx, time.Since(exportStart))
	if err != nil {
		p.handleExportFailure(records, err)
		return err
	}
	auditMetricsInstance().recordExported(ctx, int64(len(records)))
	p.storeFlushReceipts(exportResult.Receipts)
	p.currentRetryAttempt.Store(0)
	p.lastRetryTimestamp.Store(0)
	return p.removeExportedRecordsFromStore(ctx, records)
}

func (p *AuditLogProcessor) startBackgroundProcessing() {
	p.wakeExport = make(chan struct{}, 1)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.config.ScheduleDelay)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.invokeExport("Background audit export failed")
			case <-p.wakeExport:
				p.invokeExport("Audit export failed")
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
		exportStart := time.Now()
		exportResult, err := p.config.Exporter.Export(exportCtx, []Record{*record})
		auditMetricsInstance().recordExportDuration(exportCtx, time.Since(exportStart))
		if err != nil {
			exception := &AuditException{
				Message:    "Failed to export record directly",
				Cause:      err,
				Context:    ctx,
				LogRecords: []Record{*record},
			}
			p.config.ExceptionHandler.Handle(exception)
			return exception
		}
		auditMetricsInstance().recordExported(exportCtx, 1)
		p.storeFlushReceipts(exportResult.Receipts)
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
	p.queue = append(p.queue, record.Clone())
	queueSize := len(p.queue)
	p.queueMutex.Unlock()
	auditMetricsInstance().adjustQueueDepth(ctx, 1)

	if queueSize >= p.config.MaxExportBatchSize {
		p.scheduleExport()
	}

	return nil
}

func (p *AuditLogProcessor) exportLogs(ignoreRetryDelay bool) error {
	if p.shutdown.Load() && !ignoreRetryDelay {
		return nil
	}

	p.queueMutex.Lock()
	if len(p.queue) == 0 {
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
	if batchSize > len(p.queue) {
		batchSize = len(p.queue)
	}
	if batchSize > 0 {
		recordsToExport = append(recordsToExport, p.queue[:batchSize]...)
		p.queue = p.queue[batchSize:]
	}
	p.queueMutex.Unlock()

	if len(recordsToExport) == 0 {
		return nil
	}
	auditMetricsInstance().adjustQueueDepth(context.Background(), -int64(len(recordsToExport)))

	ctx := context.Background()
	exportStart := time.Now()
	if p.config.ExporterTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.ExporterTimeout)
		defer cancel()
	}

	shouldRemove := p.shouldRemoveExportedRecordsFromStore()
	exportResult, err := p.config.Exporter.Export(ctx, recordsToExport)
	auditMetricsInstance().recordExportDuration(ctx, time.Since(exportStart))
	if err != nil {
		if p.handleExportFailure(recordsToExport, err) {
			maxAttempts := p.config.RetryPolicy.MaxAttempts
			return fmt.Errorf("audit records dropped after %d retry attempts: %w", maxAttempts, err)
		}
		return err
	}
	auditMetricsInstance().recordExported(ctx, int64(len(recordsToExport)))
	p.storeFlushReceipts(exportResult.Receipts)
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
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
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

func (p *AuditLogProcessor) handleExportFailure(records []Record, cause error) bool {
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

	nextAttempt := p.currentRetryAttempt.Add(1)
	p.lastRetryTimestamp.Store(time.Now().UnixMilli())

	maxAttempts := p.config.RetryPolicy.MaxAttempts
	if maxAttempts > 0 && int(nextAttempt) > maxAttempts {
		auditMetricsInstance().recordDropped(context.Background(), int64(len(records)))
		p.markRecordsDropped(records, cause)
		p.config.ExceptionHandler.Handle(&AuditException{
			Message:    fmt.Sprintf("Failed to export audit log records after %d retry attempts", maxAttempts),
			Cause:      cause,
			Context:    context.Background(),
			LogRecords: records,
		})
		return true
	}

	p.config.ExceptionHandler.Handle(&AuditException{
		Message:    "Failed to export audit log records",
		Cause:      cause,
		Context:    context.Background(),
		LogRecords: records,
	})

	cloned := make([]Record, len(records))
	for i := range records {
		cloned[i] = records[i].Clone()
	}
	p.queueMutex.Lock()
	p.queue = append(p.queue, cloned...)
	p.queueMutex.Unlock()
	auditMetricsInstance().adjustQueueDepth(context.Background(), int64(len(cloned)))
	return false
}

func (p *AuditLogProcessor) ForceFlush(ctx context.Context) error {
	if p.config.DeliveryMode == AuditDeliveryModeSyncDirect {
		return nil
	}

	flushWait := time.NewTimer(10 * time.Millisecond)
	defer flushWait.Stop()

	for {
		p.queueMutex.Lock()
		queueLen := len(p.queue)
		p.queueMutex.Unlock()

		if queueLen == 0 {
			break
		}

		retryBefore := p.currentRetryAttempt.Load()
		exportErr := p.exportLogs(true)
		retryAfter := p.currentRetryAttempt.Load()

		p.queueMutex.Lock()
		queueAfter := len(p.queue)
		p.queueMutex.Unlock()

		if exportErr != nil {
			if queueAfter >= queueLen && retryAfter > retryBefore {
				return fmt.Errorf("failed to flush audit queue: export attempts are failing")
			}
			return exportErr
		}

		if !flushWait.Stop() {
			select {
			case <-flushWait.C:
			default:
			}
		}
		flushWait.Reset(10 * time.Millisecond)
		select {
		case <-flushWait.C:
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
	return len(p.queue)
}

func (p *AuditLogProcessor) GetRetryAttempts() int {
	return int(p.currentRetryAttempt.Load())
}
