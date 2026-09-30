## Why

Single-file configuration loaders and the pipeline currently decode layer acquisition locks independently. Their different scope validation and error handling can make the same deployed evidence mean different things to callers.

## What Changes

- Read and validate layer acquisition locks through one bounded reader, including overlays and v1 compatibility warnings.
- Keep caller-specific error identities, missing-lock policy, acquisition freshness checks, and metadata reporting at their existing boundaries.
- Reject a lock declaring the wrong layer scope before its contents can authorize remote resources, including through single-file loaders.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployed-acquisition-locks`: Clarify rejection of wrong-scope deployed evidence and preservation of independent layer evidence.

## Impact

`internal/dependencytxn`, `internal/config`, and `internal/pipeline` share one acquisition-lock reader. Existing command callers keep their error codes and lock formats.
