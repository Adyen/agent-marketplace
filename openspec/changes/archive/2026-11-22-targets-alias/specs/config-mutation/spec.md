## ADDED Requirements

### Requirement: Target list edits keep the authored spelling

CLI and TUI edits to a root target list SHALL write back the spelling the file authored, `targets:` or `harnesses:`, and SHALL write `harnesses:` when the file declares neither. An edit that does not change the target list MUST leave it as written. When the edited layer declares no list, the edit MUST start from the effective inherited list.

#### Scenario: Target list edits keep the authored spelling

- **WHEN** target lists are edited in a file that authored `targets:`, a file that authored `harnesses:`, and a file that declares neither
- **THEN** the first MUST keep `targets:`, and the other two MUST contain `harnesses:` and no `targets:` key

#### Scenario: Unrelated edit leaves an empty harnesses list as written

- **WHEN** a resource is installed into a user root that declares `harnesses: []`
- **THEN** the user root MUST still declare `harnesses: []`

#### Scenario: Editing an inherited user list copies the effective list

- **WHEN** the organization declares `harnesses: [factory, claude]`, the user root declares no list, and the user adds `cursor` in the TUI harness editor
- **THEN** the user root MUST declare `harnesses: [factory, claude, cursor]`

### Requirement: Removing the last target requires the targets spelling

Removing the last target MUST save `targets: []` only when the file already authored `targets:`. Otherwise the edit MUST be refused with guidance to upgrade every reader of the file and rename the key to `targets:`, leaving configuration and acquisition locks unchanged. An edit MUST NOT represent an empty selection by writing `harnesses: []` or deleting the key.

#### Scenario: Removing the last target from a targets list saves an empty list

- **WHEN** the user root declares `targets: [factory]` and the user removes `factory` in the TUI harness editor and saves
- **THEN** the user root MUST declare `targets: []`
- **AND** after reloading, the TUI and `status` MUST show no machine targets instead of the inherited list

#### Scenario: Removing the last target without targets spelling is refused

- **WHEN** the user root declares `harnesses: [factory]`, or declares no list while inheriting one, and the user removes every target in the TUI harness editor and saves
- **THEN** the TUI MUST show guidance to upgrade every reader of the file and rename the key to `targets:`
- **AND** the configuration file and acquisition lock MUST be byte-identical to before the save
