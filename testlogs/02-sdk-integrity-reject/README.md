# 02 SDK Integrity Reject

## Setup
- Sink: accept all
- SDK: 9 records, -reject-every 3 (every 3rd record has invalid HMAC)

## Expected behavior
- **SDK**: records 3,6,9 return status=400 with integrity rejection reason
- **Collector**: certificatelogverify rejects bad records (permanent); valid records still exported
- **Sink**: receives only the 6 valid records

## Observed
(Filled in after run)
