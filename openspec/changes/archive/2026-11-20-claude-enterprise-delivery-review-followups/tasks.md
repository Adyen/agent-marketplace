## 1. Archive and input contracts

- [x] 1.1 Verify archive contents and file modes for each resource kind; retain the raw remote MCP selector.
- [x] 1.2 Preserve ZIP path diagnostics, enforce aggregate limits before body reads, and distinguish XML tags from plain comparisons.
- [x] 1.3 Refuse malformed inline API-key values without decoding or rendering them.

## 2. Reconciliation contracts

- [x] 2.1 Preserve parent cancellation and give throttled writes back to the worker.
- [x] 2.2 Verify unchanged convergence, availability restoration, managed registry eligibility, and name claims.
- [x] 2.3 Recover a confirmed retry read-only; record pins and scan metadata for the uploaded version.

## 3. Verification

- [x] 3.1 Add focused regression tests and make the end-to-end unchanged-write check wait for pass completion.
- [x] 3.2 Reconcile operator and CLI documentation with the implemented behavior.
