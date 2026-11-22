# Proposal

## Why

Harness target lists are spelled `harnesses:`, which will not describe destinations that are not agent harnesses. Users also cannot say "this machine selects no targets": an empty user list inherits the organization list. Readers must understand a new spelling before any editor writes it, because released clients refuse unknown v4 fields.

## What Changes

- v4 roots and resource groups accept `targets:` as a lossless alias of `harnesses:` for non-empty lists. A file that declares both spellings is refused before acquisition or writes. Target IDs are validated exactly as `harnesses:` IDs are.
- In the user root only, an authored `targets: []` selects no machine targets and stops inheritance from the organization list and the repository fallback. An absent user list and `harnesses: []` keep inheriting. `targets: []` in organization or repository roots or in a resource group is refused.
- Editors keep the authored spelling and keep writing `harnesses:` when neither key exists. Editing an inherited user list first copies the effective list. Removing the last target saves `targets: []` only when the file already spells its list `targets:`; otherwise the edit is refused with migration guidance and config and lock stay unchanged.
- Existing `harnesses:` configurations resolve exactly as before.

No editor writes `targets:` into a file that did not author it, `harnesses:` is not deprecated, and no target profile behavior is added.

## Capabilities

### New Capabilities

### Modified Capabilities
- `consumer-config-format`: `targets:` alias, both-spellings refusal, empty-list placement rule, unchanged `harnesses:` resolution.
- `resource-selection`: an explicit empty user `targets:` list selects no machine targets.
- `config-mutation`: editors preserve the authored spelling, snapshot inherited lists, and refuse a last-target removal that cannot be saved as `targets: []`.

## Impact

v4 parsing and validation in `internal/config`, target-list resolution and `status` reporting, the config editors, and the TUI harness editor. Readers older than this release refuse files that contain `targets:`; teams sharing a committed repository configuration must upgrade every reader before authoring `targets:`. Acquisition locks and native output formats are unchanged.

## Release note

Configuration files can spell their target list `targets:` instead of `harnesses:`, and a user `targets: []` selects no machine targets. Older CLI releases refuse `targets:`, so upgrade every client that reads a file before using it.
