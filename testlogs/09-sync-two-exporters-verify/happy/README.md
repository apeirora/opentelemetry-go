# 09 Sync Two Exporters â€” Happy Path

## Setup
- Sink: accept all
- SDK: 3 records, sync export

## Expected behavior
- SDK: 3/3 status=200 delivered
- Both collector exporters (debug + otlp_http) complete synchronously

## Observed
(Filled in after run)
