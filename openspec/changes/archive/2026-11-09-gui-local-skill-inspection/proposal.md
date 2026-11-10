# Proposal

## Why

In the desktop GUI, Enter and the Open resource button open Marketplace skills in the inspector but are not offered for skills from directory and Git sources, so users cannot read those skills' `SKILL.md` and supporting files before or after installing them.

## What Changes

- Directory and Git skill rows whose source content is available offer Enter and Open resource, and the inspector opens on `SKILL.md` with the skill's supporting files browsable.
- The inspector reads those files read-only from the captured source root of the source, honoring the registry symlink containment rules: a symlink that stays inside the source root is followed, while missing files, symlinks that escape the source root, and special files are refused without reading their bytes.
- A refused or missing local source shows an actionable error, reported by the local API as `localapi.source_files_unavailable`.
- Marketplace inspection and installed Marketplace skill browsing are unchanged.

## Capabilities

### New Capabilities

### Modified Capabilities
- `gui-shell`: adds read-only inspection of directory and Git skill rows.

## Impact

Local API row capabilities and the installed skill files route, the local API contract (`api/gui-local.yaml`), the GUI inspector error copy, and the GUI browser tests. The CLI and TUI are unaffected.

## Release note

The desktop GUI now opens skills from directory and Git sources in the inspector, showing `SKILL.md` and its supporting files without changing anything on disk.
