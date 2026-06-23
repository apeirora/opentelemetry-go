# Audit Log E2E Manual Tests

Local end-to-end tests wiring three components:

| Component | Path | Role |
|-----------|------|------|
| **SDK testapp** | `sdk/auditlog/testapp` | Emits signed audit records (HMAC + cert) over OTLP/HTTP mTLS |
| **Collector** | `opentelemetry-collector-contrib` `otelauditcol` + `example-config.yaml` | Receives on `:4310/v1/audit`, verifies integrity, exports to debug + backend |
| **Flaky sink** | `opentelemetry-collector-contrib/test-standalone` | Simulates OTLP/HTTP log backend on `:9999` with configurable rejects/delays |

## Configuration of whole flow

Rules for SDK + collector + backend wiring (see also `example-config.yaml` header comments):

1. **Split durability by failure type** — When the collector is reachable, it owns durability (receiver WAL + sync pipeline). The SDK store/retry queue is used **only when the collector is unreachable** (connection failure, timeout, DNS). Do not persist and retry on both sides for the same record: HTTP responses from a reachable collector (200/400/503) are handled synchronously by the app; the SDK does not keep a competing backlog for those cases.

2. **Async is unsafe for now** — Stay sync end-to-end until async paths are hardened:
   - SDK: synchronous export when the collector is reachable (`WaitOnExport=true` in testapp); background store-and-retry only when the collector is unreachable
   - Collector receiver: `response_mode: sync` (not `async` / 202 accept-then-deliver)
   - Processor: `certificatelogverify` `mode: sync`
   - Exporters: `sending_queue.enabled: false` (no background export queue)

3. **SDK store** — A store is always required (in-memory by default in testapp; use `-filestore` for durable recovery when the collector is offline). Records are persisted only on connection failure, not on HTTP rejections from a reachable collector.

4. **Collector receiver storage = WAL only** — `redis_storage` / `file_storage` is write-ahead log (persist before pipeline, delete on success). `recoverSyncPending` runs on restart; it is not a competing in-request retry worker.

5. **Propagate backend failures to the SDK** — With `sending_queue: false`, exporter `retry_on_failure` retries inline within the HTTP request (bounded by `max_elapsed_time`). HTTP 503 to the SDK means end-to-end delivery failed; the app must re-emit or escalate.

6. **HTTP semantics** — `200` = verified and exported through the full sync pipeline. `400` = integrity/verification rejected (permanent). `503` = transient pipeline or backend failure.

7. **Do not combine** — SDK `-filestore` durable backlog + collector receiver WAL with competing retry semantics; collector `response_mode: async` + SDK offline store-and-retry without clear ownership.

**Recommended pairing (this test suite):** SDK sync export when collector is up → collector `response_mode: sync` + receiver WAL → sync processor → sync exporters.

## Architecture

```
testapp (SDK)
    │  OTLP/HTTP mTLS
    ▼
auditlogreceiver :4310  (sync, Redis WAL)
    │
    ▼
certificatelogverifyprocessor  (HMAC + cert, strict)
    │
    ├──► debug exporter (stdout in collector.log)
    └──► otlphttp/test_standalone → flaky sink :9999
```

### SDK (`testapp`)

- **Delivery**: synchronous export on each emit when the collector is reachable; store-and-retry in the background only when the collector is unreachable. `WaitOnExport=true`, 10s exporter timeout.
- Store: in-memory by default; `-filestore` for durable offline recovery.
- Signs each record with HMAC-SHA256 (`dev_hmac_key.txt`) and attaches cert metadata.
- Flags: `-count`, `-interval`, `-reject-every N` (invalid integrity every Nth emit), `-otlp-endpoint`.
- **400** from collector = integrity rejected before pipeline export.
- **200** from collector = record accepted by receiver after verification (does **not** guarantee backend sink delivery — see findings below).

### Collector (`example-config.yaml`)

- **Receiver**: HTTPS mTLS on `0.0.0.0:4310`, path `/v1/audit`, `response_mode: sync`, Redis storage for WAL.
- **Processor**: `certificatelogverify` — sync strict HMAC verification, dead-letter queue in Redis.
- **Exporters**: `debug` (detailed) + `otlphttp` → `http://localhost:9999`.
- Exporters use `sending_queue: false` + inline `retry_on_failure` (bounded `max_elapsed_time`) so backend failures surface as **503** to the SDK within the same HTTP request.

### Flaky sink (`test-standalone`)

Flags added for testing:

- `-reject-every N` — reject every Nth request (0 = accept all)
- `-reject-code` — HTTP status for rejects (503, 429, …)
- `-delay` — sleep before responding (timeout simulation)

## Prerequisites

- Redis on `127.0.0.1:6379` (collector `redis_storage` extension)
- Built binaries:
  - `opentelemetry-collector-contrib/bin/otelauditcol_windows_amd64.exe`
  - `opentelemetry-collector-contrib/test-standalone/flaky-otlp-backend.exe`
- Run collector from `opentelemetry-collector-contrib` root (config uses relative TLS/key paths)

## Re-run all scenarios

```powershell
powershell -ExecutionPolicy Bypass -File testlogs/run-e2e-scenarios.ps1
```

## Scenarios

| # | Folder | Summary |
|---|--------|---------|
| 01 | [01-happy-path](01-happy-path/README.md) | All records accepted end-to-end |
| 02 | [02-sdk-integrity-reject](02-sdk-integrity-reject/README.md) | SDK sends bad HMAC → 400 |
| 03 | [03-backend-503-intermittent](03-backend-503-intermittent/README.md) | Sink rejects every 3rd with 503 |
| 04 | [04-stress-throughput](04-stress-throughput/README.md) | 30 records @ 10ms interval |
| 05 | [05-backend-always-reject](05-backend-always-reject/README.md) | Sink always 503 |
| 06 | [06-backend-429-rate-limit](06-backend-429-rate-limit/README.md) | Sink rejects every 2nd with 429 |
| 07 | [07-partial-sdk-and-backend](07-partial-sdk-and-backend/README.md) | Combined integrity + backend failures |
| 08 | [08-backend-slow-timeout](08-backend-slow-timeout/README.md) | Sink 12s delay |
| 09 | [09-sync-two-exporters-verify](09-sync-two-exporters-verify/README.md) | Fixed config: 2 sync exporters, 503 propagated |

## Retest results (sync config applied)

| # | Scenario | SDK result | Sink result | Status |
|---|----------|------------|-------------|--------|
| 01 | Happy path | 5/5 delivered | 5 accepted | ✅ |
| 02 | Integrity reject | 6 delivered, 3×400 | 6 accepted | ✅ |
| 03 | 503 every 3rd | 9/9 delivered | 9 accepted (after inline retry) | ✅ |
| 04 | Stress 30 | 30/30 delivered | 30 accepted | ✅ |
| 05 | Always 503 | 3/3 **503** | 0 accepted | ✅ fixed |
| 06 | 429 every 2nd | 8/8 delivered | 8 accepted (after inline retry) | ✅ fixed |
| 07 | Mixed failures | 9 delivered, 3×400 | 9 accepted | ✅ |
| 08 | 12s delay | 2/2 **503** timeout | no accept | ✅ fixed |

## Key finding (scenarios 01–08, **before** config fix)

The collector **sync receiver returns HTTP 200 to the SDK after verification passes**, while the `otlphttp` exporter to the flaky sink runs with **background retry**. Therefore:

- SDK `status=200 delivered` ≠ guaranteed delivery to the final backend.
- Sink `accepted` count is the ground truth for backend delivery within the test window.
- For durability when the collector is **unreachable**, use SDK `-filestore` (connection failures are stored and retried). HTTP errors from a reachable collector are logged and not stored (not exercised in scenarios 01–08).

Each scenario folder contains `testapp.log`, `collector.log`, `sink.log`, `meta.txt`, and `README.md`.

## Config fix for 100% sync (2 exporters)

See `opentelemetry-collector-contrib/receiver/auditlogreceiver/example-config.yaml` and scenario **09**. Both `debug` and `otlp_http/test_standalone` use `sending_queue.enabled: false` so fan-out export errors propagate to the SDK as 503.
