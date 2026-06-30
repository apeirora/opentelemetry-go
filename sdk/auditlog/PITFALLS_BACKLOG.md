# Auditlog pitfalls backlog

Active design: sync collector ownership when reachable; SDK store-and-retry **only** on transport failure.

## Resolved

- [x] Connection failure → `503` / `stored` / `collector_unreachable_stored`
- [x] Strict startup verify + TLS (`WithStrictStartupVerify`)
- [x] OTLP inline HTTP retry (~1 retry per `Export()`)
- [x] `AuditException.Status` for handler branching
- [x] Unified delivery model (no delivery/storage mode switches)
- [x] **Stored-then-HTTP-fail purge** — store entries are no longer removed on background HTTP rejection; `RemoveAll` only after successful export
- [x] **Background HTTP retry** — stored batches re-queued with `RetryPolicy` backoff on HTTP 503/429 during background export
- [x] **HTTP 503 ≠ offline** — reachable collector HTTP 503/429 on emit is not stored (`503 rejected`); transport failures on emit are stored (`503 stored`). Background export of stored records retries on HTTP 503/429 with backoff; `RemoveAll` only after HTTP 2xx. Emit-time HTTP errors are never persisted.
- [x] **Export circuit after `MaxAttempts`** — circuit opens, resyncs store to queue after cooldown, and probes again without requiring process restart

## P1 — accepted / deferred

### Duplicate delivery at sink

If export succeeds but `RemoveAll` fails, or the process restarts before compaction, replay may send the same `audit.record.id` again. **Deferred:** customer handles idempotency at collector/sink endpoints.

### In-memory default store

No crash recovery without `-filestore` or another durable backend.

### `200 delivered` without sink receipt

`SinkTimestamp` may fall back to `time.Now()` when the exporter returns no receipt.

### Dual durability (SDK + collector WAL)

See `testlogs/README.md` — avoid competing retry semantics.

### Unclassified exporter errors

Custom exporters should return OTLP-shaped HTTP errors or underlying `net.OpError`/timeout for correct classification (`export_errors.go`).

## P2 — performance and scale (elaboration)

These are not correctness bugs; they bound throughput and resource use under load or long outages.

### Per-record sync export (happy path)

Every `OnEmit` calls `Exporter.Export` with a single-record batch when the collector is reachable. There is no emit-side batching. Throughput ≈ `1 / (RTT + marshal + TLS)` per goroutine. High-volume apps should expect more collector HTTP requests than a batched telemetry pipeline.

### Global `currentRetryAttempt` backoff

One atomic retry counter and backoff schedule is shared across **all** queued batches. A slow or failing head batch increases delay before later batches export (head-of-line blocking). Fine for modest queue depth; painful during extended partial outages with large backlogs.

### Queue + store memory during outages

On transport failure the processor `Save`s to the store **and** clones into the in-memory queue. Until export succeeds and `RemoveAll` runs, the same record exists in both structures (~2× memory for pending audit lines).

### `ForceFlush` 10ms polling loop

`ForceFlush` spins with a 10ms timer while the queue is non-empty and export is waiting on retry backoff (`ignoreRetryDelay` still respects in-flight export). Under heavy retry backpressure, flush/shutdown may wake frequently instead of sleeping until the next retry window.

**Mitigations (future, not implemented):** optional emit-side micro-batching when `WaitOnExport: false`, per-batch retry state, queue-only or store-only pending path to halve memory, backoff-aware flush wait.

## P3 — testing, ops, docs

- [x] `AUDIT_LOG_README.md` — delivery + store removal contract updated
- [x] `testlogs/README.md` — historical note clarified; scenario 09 as sync reference
- [ ] Re-run `testlogs/run-e2e-scenarios.ps1` to refresh captured logs (still contain old `sync_direct` banner from pre-unified model)

### Exception handler vs emit status

`DefaultAuditExceptionHandler` logs to `otel.Handle` only. Production apps should branch on `EmitWithResult` status and `AuditException.Status`, not only on log lines.

### Stresstest vs mTLS

`mockreceiver` is plain HTTP; full chain is covered in `otlpexport/verify_test.go`.
