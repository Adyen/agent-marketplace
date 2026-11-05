## 1. Regression coverage

- [x] Add failing fsboundary coverage for symlink and non-directory intermediate components naming the absolute path and reason.
- [x] Add failing materialization coverage for unsafe output ancestors naming the canonical path and reason.

## 2. Implementation

- [x] Name captured descriptors with their full path and classify refused intermediate components by their no-follow shape.
- [x] Include link, scope, and destination paths in harness root and output-directory refusals.

## 3. Verification

- [x] Run scoped fsboundary, harness, and materialize tests, the spec check, and lint.
