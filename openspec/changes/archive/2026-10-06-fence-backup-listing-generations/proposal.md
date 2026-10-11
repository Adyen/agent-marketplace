## Why

ListBackups enumerates target directories then reopens their names by path for archive globbing or Codex manifest discovery. A late foreign alias supplies foreign backup names to a successful listing.

## What Changes

- Enumerate below captured backup-root descriptors and capture each target's no-follow metadata/object identity before reading members.
- Match target descriptors to admitted generation and reuse captured fs.Glob plus loadCompleteCodexSetsAt, closing per-target descriptors within each iteration rather than retaining them across all targets.
- Revalidate named target directory before returning claims, preserving optional root absence, ordinary empty targets, Codex malformed/incomplete filtering and supported initial backup-root aliases.

## Capabilities

### Modified Capabilities

- `backup-recovery`: backup listing claims require admitted target-directory generation authority.

## Impact

R2 child of #572, stack #560. Existing directory/metadata/parent identity and Codex manifest helpers reused, no new helper/type/public API/dependency/budget/source-comment/content-policy/durability change. Listing remains read-only; this does not establish an action lease for later Restore/Rotate, complete directory snapshots or atomic uncooperative-writer exclusion. Final admitted-case audit/appendix stays open.
