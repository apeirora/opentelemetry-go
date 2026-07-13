# 03 Backend 503 Intermittent

**Retest:** 2026-06-18 (sync config with inline `retry_on_failure`)

## Setup
- Sink: reject every 3rd request with 503
- SDK: 9 valid records, 100ms interval

## Observed ✅
- **SDK**: 9/9 `status=200 delivered`
- **Sink**: initial rejects on req 3,6,9; **inline collector retries** succeeded — final `accepted=9` (13 HTTP requests total including retries)
- **Before fix**: SDK showed 200 but sink only `accepted=6` (retries were async/background)

Sync + inline retry now means SDK 200 matches backend delivery when retries succeed within `max_elapsed_time` (8s).
