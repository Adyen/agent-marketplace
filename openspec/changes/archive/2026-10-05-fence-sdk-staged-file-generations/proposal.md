## Why

SDK root/staging-directory authority does not identify staged files. Replacing a staged payload after writing lets SDK publish foreign bytes; refusal cleanup can remove the replacement below the still-original directory.

## What Changes

- Retain staged payload full identity/state from its actual opened writer descriptor after writing/chmod, including failed writes where inspectable.
- Require full staged state before publication rename.
- Validate tracked staged files before any directory cleanup; preserve replacements/changed state instead of deleting them. Published/missing payloads remain absent.
- Refused stage containing a replacement is intentionally retained rather than silently removed.

## Capabilities

### Modified Capabilities

- `materialization`: original staged-file publication and refusal-cleanup authority.

## Impact

R2 child of #546 in stack #478. Existing fstat/Identity/descriptor rename/cleanup reused; no helper/type/public API/dependency/budget/source-comment change. All-artifact staging/modes/no-op/dry/partial compatibility retained. Stale discovery/removal and final admitted-case audit remain separate. No whole-tree transaction or atomic uncooperative-writer exclusion.
