## Why

Collection and Marketplace removal return before ordinary removal candidates
are resolved. A same-named explicit missing resource can therefore be ignored
while another selection is removed, contrary to the removal ambiguity contract.

## What Changes

- Admit mixed explicit registry and early-removal selections through existing
  kind, source, and scope disambiguation before editing.
- Apply the same kind check to named `--from` collection removal and honor an
  explicit non-collection kind.
- Keep single-selection cleanup independent of catalog availability.
- Assert exact parent-registry survivors rather than only their count.

## Capabilities

### Modified Capabilities

- `resource-removal`: enforce ambiguity admission before early removal paths.

## Impact

Shared removal pipeline, regression tests, and public CLI E2E coverage.
No configuration, acquisition-lock, or materialization format changes.
