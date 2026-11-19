## Why

The delivery pipeline needs one end-to-end verification that a reviewed
branch can be published as a draft pull request. This change adds a
documentation note that marks the branch as a disposable verification, not a
product change.

## What Changes

- Add `docs/relay-publication-verification.md`, a short note stating that
  the branch is a disposable verification of reviewed branch publication and
  is not a product change.

## Capabilities

### New Capabilities

None. This change adds documentation prose only.

### Modified Capabilities

None. Runtime behavior and normative requirements remain unchanged.

## Impact

- Documentation only; no code, spec, or configuration changes.
- The note contains no internal hostnames, credentials, or other
  organization data.
