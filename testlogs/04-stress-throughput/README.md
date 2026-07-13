# 04 Stress Throughput

## Setup
- Sink: accept all
- SDK: 30 records, 10ms interval

## Expected behavior
- All 30 records delivered without integrity errors
- Collector circuit breaker stays closed under moderate load

## Observed
(Filled in after run)
