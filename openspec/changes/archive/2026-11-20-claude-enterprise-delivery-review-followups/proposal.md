## Why

Claude Enterprise delivery must distinguish a stored version from a served version, preserve operator settings, and avoid retrying a write without evidence. Review also identified invalid archive and configuration inputs that can produce the wrong diagnostic.

## What changes

- Recover confirmed version writes without retrying registration across passes. Report operator pins and read scan status for the uploaded version.
- Preserve paused registry publications, exclude withdrawn registries, and restore a returning resource's availability without uploading identical bytes.
- Keep parent cancellation out of per-plugin failure state and route throttled writes through the worker cooldown.
- Validate archive limits before reading entries, preserve remote MCP transport, and reject inline API keys before decoding their payload.

## Capabilities

### Modified capabilities

- `claude-enterprise-delivery`

## Impact

Delivery worker, Claude Plugins API client, delivery state in SQLite and PostgreSQL, operator documentation, and tests. No consumer configuration changes, group setting writes, pin changes, or durable resend permission.

## Release note

Claude Enterprise delivery now reports when a different version remains pinned and keeps scan status tied to the upload. It also restores an unchanged returning resource without uploading another version.
