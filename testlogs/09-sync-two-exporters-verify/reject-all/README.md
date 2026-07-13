# 09 Sync Two Exporters â€” Reject All

## Setup
- Sink: reject every request with HTTP 503
- SDK: 2 records

## Expected behavior
- SDK: status=503 rejected after inline collector retries exhaust
- Sink: ccepted=0

## Observed
(Filled in after run)
