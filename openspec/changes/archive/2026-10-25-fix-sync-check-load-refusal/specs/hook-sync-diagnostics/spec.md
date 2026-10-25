## ADDED Requirements

### Requirement: Sync checks distinguish load refusal from hook drift

When configuration, registry loading, or resource resolution has a hard error
that prevents ordinary `sync` from materializing, `sync --check` SHALL refuse
before predicting materialized output changes. It SHALL report the actual
refusal diagnostics and SHALL NOT print a pending hook review, a deselection
explanation, or an output drift count derived from the incomplete inventory.
Installed hook files and existing ownership state SHALL retain their bytes,
modes, and modification times. This does not make warning-only inspection fatal
or suppress genuine hook drift after successful loading and resolution.

#### Scenario: Unloadable hook source refuses without pending changes

- **GIVEN** a selected hook is already installed
- **WHEN** its registry cannot load because the source contains ambiguous modeled fields
- **THEN** `sync` and `sync --check` MUST refuse with `json.duplicate-key`, preserve installed files and existing ownership state including bytes, modes, and modification times, and report no pending hook removal, deselection explanation, or output drift count

#### Scenario: Loadable hook sources retain genuine drift reports

- **GIVEN** an installed hook whose source loads successfully
- **WHEN** the hook changes or is deselected
- **THEN** `sync --check` MUST report the genuine replacement or removal and a nonzero drift count without changing installed files or ownership state, and subsequent `sync` MUST apply the reviewed operation
