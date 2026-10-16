## ADDED Requirements

### Requirement: Locally edited file-mode output is reported as an edited managed copy

When a harness target uses file materialization mode and a selected resource's output is recorded in the materialization manifest with a digest, and the bytes on disk match neither that digest nor the output sync would write now, sync and `sync --check` SHALL preserve those bytes and the manifest entry and SHALL report one `materialize.collision.user-owned` warning. Its message MUST say the output is a locally edited copy of a resource managed by Agent Marketplace, MUST name the output path and the resource, MUST say that deleting or moving the copy and running sync restores the registry version, and MUST NOT describe the output as not managed by Agent Marketplace. The same message SHALL be used when such a copy prevents the harness target from returning to cache-backed symlinks. Output that the materialization manifest does not record MUST keep the existing unmanaged-collision message with its remove and rename hint. Exit codes are unchanged.

#### Scenario: Edited file-mode skill copy is reported as a local edit

- **WHEN** a skill copied in file materialization mode and recorded in the materialization manifest is edited, then sync or `sync --check` runs
- **THEN** one `materialize.collision.user-owned` warning names the copy's path and resource as a locally edited managed copy with the restore hint and without saying it is not managed, and the edited bytes and manifest entry are unchanged

#### Scenario: Edited file-mode single-file copy is reported as a local edit

- **WHEN** a single-file resource output copied in file materialization mode and recorded in the materialization manifest is edited, then sync runs
- **THEN** one `materialize.collision.user-owned` warning names the file's path and resource as a locally edited managed copy with the restore hint, and the edited bytes are unchanged

#### Scenario: Edited copy blocking a return to symlinks is reported as a local edit

- **WHEN** a recorded file-mode copy is edited and the harness target's materialization mode changes back to symlinks before sync runs
- **THEN** sync keeps the edited copy instead of linking it and reports the same locally edited managed copy warning

#### Scenario: Unrecorded existing output keeps the unmanaged collision warning

- **WHEN** a file or directory that the materialization manifest does not record already exists where a selected resource would be written, then sync runs
- **THEN** sync preserves it and reports the existing `materialize.collision.user-owned` message that it is not managed by agent-marketplace, with the remove and rename hint

#### Scenario: Removing an edited copy restores the registry version

- **WHEN** a locally edited recorded file-mode copy is deleted and sync runs again
- **THEN** sync writes the registry version to that path, records it in the materialization manifest, and reports no collision for it
