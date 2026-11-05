# Proposal

## Why

Public payload routes currently check their existing byte ceilings after an entire stored blob has been fetched. A much larger stored object can therefore consume memory and backend bandwidth even though the response is refused.

## What Changes

- Apply existing inspection, file and manifest byte ceilings while reading from the built-in blob backends.
- Preserve accepted responses, availability checks, digest verification and existing refusal status codes.
- Retain compatibility with custom blob stores that implement only the existing read interface; these stores remain responsible for their own read confinement.
- Add bounded byte-counting HTTP regressions and a healthy content-read benchmark.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: apply existing payload ceilings before fully reading oversized stored objects.

## Impact

Marketplace payload readers and their filesystem/S3 blob-store integration. No new thresholds, public interfaces, dependency versions or wire formats.
