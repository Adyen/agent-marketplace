## Why

The delivery review suggested aborting a stalled web healthcheck request
explicitly instead of depending solely on Docker to terminate its process.

## What Changes

- Give the native Node fetch a four-second AbortSignal deadline, before the
  existing five-second Docker healthcheck timeout.
- Retain exec-form invocation and nonzero exits for failed or aborted requests.
- Add a regression assertion and exercise successful, failed, and stalled
  HTTP responses using the healthcheck extracted from the actual Dockerfile.
- Document the existing Helm values upgrade and test previous values files
  before and after explicit image pins, without changing release versions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-delivery-security`: Explicit web healthcheck request deadlines.

## Impact

Only the supplied web image's healthcheck changes. No CLI, HTTP API, consumer
configuration, acquisition-lock, or harness output changes.
No release version, tag, or release-policy changes are included.
