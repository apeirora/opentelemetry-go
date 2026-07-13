# 05 Backend Always Reject

## Setup
- Sink: reject every request (-reject-every 1) with 503
- SDK: 3 valid records, 500ms interval

## Expected behavior
- **Collector**: cannot complete export to backend; returns 503 to SDK
- **SDK**: status=503 (or export failure) â€” HTTP errors from a reachable collector are not stored locally
- **Sink**: ccepted=0, all requests REJECTED

## Observed
(Filled in after run)
