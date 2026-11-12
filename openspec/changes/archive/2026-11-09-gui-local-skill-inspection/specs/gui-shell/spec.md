## ADDED Requirements

### Requirement: Read-only directory and Git skill inspection

When a directory or Git skill row's source content is available, the GUI inspector SHALL open it with `SKILL.md` previewed first and the skill's supporting files browsable. It SHALL read those files only from the captured source root, following a symlink only when it resolves inside that root, and SHALL NOT write configuration, acquisition locks, caches, or materialized files. When the content is missing, a file escapes the source root through a symlink, or a file is a special file, the inspector SHALL show an actionable error, the local API SHALL report `localapi.source_files_unavailable`, and no bytes from outside the source root SHALL be returned.

#### Scenario: Directory and Git skills open read-only in the inspector

- **WHEN** a user opens a directory skill row and a Git skill row whose source content is available, with Enter or with Open resource
- **THEN** the inspector MUST preview `SKILL.md` first, MUST list the supporting files and preview a selected one, and MUST leave configuration, acquisition locks, and materialized files unchanged

#### Scenario: Unsafe or missing local skill source fails actionably

- **WHEN** a user opens a directory or Git skill row whose source content is missing, or whose file escapes the source root through a symlink or is a special file
- **THEN** the inspector MUST show an actionable error that directs the user to fix the source and refresh, and the response MUST NOT contain bytes from outside the source root
