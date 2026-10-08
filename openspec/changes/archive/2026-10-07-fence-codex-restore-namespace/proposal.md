## Why

After archive preflight/verification, RestoreCodexSet still prepares partition roots with path-based MkdirAll and extraction recaptures their paths. Retargeting an admitted missing-root parent creates a foreign root/native file; conflict is noticed only after that first partition changed foreign bytes.

## What Changes

- Capture each existing partition root or nearest existing parent before preparation; revalidate named generation before descriptor-relative missing-root creation.
- Carry prepared captured root through existing extraction rather than reopening its path; ordinary restore preserves its existing own-capture lane.
- Preserve parent aliases, genuine missing/nested roots, unsafe-root refusal, archive/native modes, evidence/partial-error contracts and real two-partition recovery.

## Capabilities

### Modified Capabilities

- `backup-recovery`: Codex restore namespace/root preparation retains original generation through extraction.

## Impact

R2 child of #584, stack #560. Existing capture/EnsurePath/parent checks/extraction reused; private extractor accepts borrowed root descriptor, no new helper/type/public API/dependency/budget/source-comment/content-policy/durability change. Borrowing does not create a full output transaction, all-directory rollback, complete input/namespace snapshot or atomic uncooperative-writer exclusion. Remaining ownership/admitted-case/report-appendix audit stays open.
