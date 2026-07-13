# 08 Backend Slow / Timeout

## Setup
- Sink: -delay 12s (exceeds collector exporter timeout)
- SDK: 2 records

## Expected behavior
- SDK: status=503 rejected (timeout while collector waits on slow backend)
- Sink: requests received but not accepted before timeout

## Observed
(Filled in after run)
