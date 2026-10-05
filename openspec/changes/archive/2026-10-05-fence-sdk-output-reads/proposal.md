## Why

SDK Materialize compares existing artifact bytes using os.ReadFile. Multiply linked output can authorize a no-op, and a FIFO can block before staging. CLI wrappers cannot establish a safe SDK read if another public caller bypasses their checks.

## What Changes

- Reuse the existing descriptor-bound regular reader for SDK artifact comparisons.
- Bind classification and bytes to one unchanged file identity with captured-size growth checks.
- Preserve genuine absence, byte comparison, regular no-op identity/mode and real updates.

## Capabilities

### Modified Capabilities

- `materialization`: SDK artifact-comparison reads.

## Impact

R2 child of #540 in stack #478. Shared private fileMatchesBytes fixes all SDK comparison callers. No public API/type/helper/dependency/new absolute budget/source-comment changes. SDK/root readers use existing internal fsboundary in the embedded root module. Public SDK consumer proves refusal; cache wrapper already refused the initial native test before reaching SDK, so it is not cited as a reproducer. Publisher/stale-discovery and final admitted-case audit remain separate.
