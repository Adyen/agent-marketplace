## Why

Codex backup-set manifest partial writes follow pre-existing aliases and truncate foreign metadata. Set archive final renames and refusal cleanup also re-resolve paths independently of the captured archive output authority.

## What Changes

- Capture one Codex backup-set directory for name reservation, archive/manifest partial writes, member publication and refusal cleanup.
- Preserve existing partial names by selecting an unused suffixed operation with descriptor-relative no-follow metadata inspection.
- Create private partials exclusively, retain producer-observed identities, and validate staged member state, final absence and named directory before descriptor-relative publication.
- Remove only tracked attempt-created inodes on refusal.

## Capabilities

### Modified Capabilities

- `backup-recovery`: Codex backup-set publication and cleanup authority.

## Impact

Dependent on #506 in R2 stack #478. Preserve manifest-last completeness, root-key matching, archive format, private archive modes and manifest0644 mode. Namespace directory preparation, retention and remaining ownership/recovery consumers remain separate. No added durability sync, size/work budget, all-output transaction or exclusion of uncooperative writers.
