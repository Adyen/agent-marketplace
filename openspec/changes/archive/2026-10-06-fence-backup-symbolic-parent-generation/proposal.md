## Why

Symbolic restoration can retain a valid original parent descriptor after its public pathname is replaced. Leaf checks under that detached descriptor still pass, so restore deletes the original held native leaf and reports success despite the foreign named parent.

## What Changes

- Re-admit the currently named parent with the existing captured-directory boundary and require its object identity to match the original before symbolic leaf removal/link creation.
- Preserve foreign and original parent/leaf bytes, identities, modes, mtimes and membership with typed generation refusal.
- Retain supported initial parent aliases, strict leaf admission and real archived symbolic target restoration.

## Capabilities

### Modified Capabilities

- `backup-recovery`: symbolic restore mutation requires original named parent generation.

## Impact

R2 child of #569 in stack #560. Existing helper and directory identity admission reused; no new helper/type/API/dependency/budget/source-comment change. No recursive descendant snapshot, atomic writer exclusion or transactional symbolic replacement claim. Remaining discovery/action authority and final case audit remain separate.
