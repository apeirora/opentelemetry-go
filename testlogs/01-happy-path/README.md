# 01 Happy Path

## Setup
- Sink: accept all (-reject-every 0)
- SDK: 5 valid records, sync export when collector is up, mTLS to collector :4310

## Expected behavior
- **SDK**: each emit returns status=200 delivered
- **Collector** (uditlogreceiver sync): verifies HMAC via certificatelogverify, forwards to debug + OTLP HTTP sink
- **Sink**: 5 log records accepted (1 per HTTP request in sync mode)

## Observed
(Filled in after run â€” see testapp.log, collector.log, sink.log)
