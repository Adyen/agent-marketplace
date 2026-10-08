## Why

Regular restore validates a written staged payload before archived chmod, closes its descriptor and later renames the staged pathname. A staged-file replacement after validation can publish foreign bytes and report successful recovery.

## What Changes

- Match the final staged name below the captured output parent to the completed descriptor identity immediately before rename, using existing no-follow metadata and full state comparison.
- Preserve current native output and foreign replacement stages with typed generation refusal; remove only attempt-owned staged inodes.
- Retain ordinary, mapped native and Codex restore flows, genuine archive-mode restoration and unreadable archived modes without reopening their payloads after chmod.

## Capabilities

### Modified Capabilities

- `backup-recovery`: completed regular restore staging identity remains authoritative through publication.

## Impact

R2 child of #564 in continuation stack #560. Shared existing publisher and identity operations reused; existing test observer expanded without a new test helper/type. No public API/dependency/budget/source-comment change, durability sync or extraction transaction. Original output destination/parent late action binding and symbolic restore remain separately audited. Final admitted-case audit/appendix stays open.
