## Why

CAS-refusal cleanup validates the safety attempt directory before member deletion, then removes the empty directory by name. Replacing that admitted empty directory at final cleanup makes real Apply remove the foreign replacement while still returning the original policy refusal.

## What Changes

- Recheck the safety attempt's original child generation and named parent immediately before Rmdir using existing captured operations/checker.
- Preserve foreign empty/nonempty/alias directories on cleanup refusal and retain native state plus original error/receipt semantics.
- Keep genuine owned empty-directory cleanup and real subsequent Apply publication.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: empty safety-directory cleanup retains original directory generation at removal.

## Impact

R2 child of #588, stack #560. Existing OpenPath/identity/named-state-parent/Rmdir reused; no new production helper/type/public API/dependency/budget/source-comment/content/durability policy. Best-effort empty-directory cleanup reports through existing warning behavior; original policy errors remain primary. No new transaction rollback, complete snapshot or atomic uncooperative-writer exclusion promise. Remaining case audit/report appendix stays open.
