# 06 Backend 429 Rate Limit

## Setup
- Sink: reject every 2nd request with HTTP 429
- SDK: 8 valid records, 150ms interval

## Expected behavior
- Collector treats 429 as retryable; eventual delivery of all 8 records
- Sink shows alternating REJECTED 429 / ACCEPTED 200

## Observed
(Filled in after run)
