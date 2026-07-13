# 09 Sync Two Exporters — Config Verification

Validates the updated `example-config.yaml` with **two synchronous exporters** (`debug` + `otlp_http/test_standalone`).

## Config changes applied

Both exporters disable async queue; backend exporter retries inline within SDK timeout budget:

```yaml
exporters:
  debug:
    verbosity: detailed
    sending_queue:
      enabled: false
  otlp_http/test_standalone:
    endpoint: http://localhost:9999
    timeout: 8s
    sending_queue:
      enabled: false
    retry_on_failure:
      enabled: true
      initial_interval: 500ms
      max_interval: 2s
      max_elapsed_time: 8s
```

Pipeline still fans out to **both** exporters. Fan-out fails if **either** exporter fails synchronously.

## Cases

### happy (sink accept all)
- **SDK**: 3/3 delivered (quiet mode)
- **Expected**: 200 for all emits

### reject-all (sink always 503)
- **SDK**: `status=503 rejected` for both records
- **Reason** includes `HTTP Status Code 503` and `no more retries left` after inline retries exhausted
- **Before fix**: SDK incorrectly showed `200 delivered` while sink rejected everything

## Conclusion

With `sending_queue.enabled: false` on both exporters, sync receiver mode now provides **end-to-end sync semantics** across the 2-exporter fan-out path.
