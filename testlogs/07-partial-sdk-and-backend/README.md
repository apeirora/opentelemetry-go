# 07 Partial SDK + Backend Failures

**Retest:** 2026-06-18 (sync config)

## Setup
- Sink: reject every 4th with 503
- SDK: 12 records, integrity fail every 4th, 100ms interval

## Observed ✅
- **SDK integrity**: records 4,8,12 → `status=400 rejected`; 9 valid → `status=200 delivered`
- **Sink**: `accepted=9` for 9 valid records (inline retries recovered transient 503s on req 4,8)
- Both failure modes work independently and correctly
