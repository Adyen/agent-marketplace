# Design

## Context

`harnesses:` is parsed at the root (`internal/config/v4.go`) and inside resource groups, validated against known target IDs (`v4.harness`), and resolved per selecting scope (`resource-selection`). `SetHarnessesInConfig` rewrites the root list and refuses an empty list; the TUI harness editor refuses saving no targets with "select at least one harness". Repository personal configuration cannot declare a list.

## Decisions

1. **Alias.** `targets` and `harnesses` are the same field. A root or group declaring both refuses with a `v4` diagnostic naming the file and both fields, before acquisition, lock or harness work. IDs follow existing validation and diagnostics.
2. **Empty list.** Only the user root may author `targets: []`; it counts as a declared, empty machine list: user and organization selections get no targets, and repository selections that fall back to the user list get none. `targets: []` elsewhere refuses with `v4.harness`. Absent user lists and `harnesses: []` keep current inheritance and the repository fallback. Deleting the user key restores inheritance.
3. **Editing.** Editors record which spelling the file authored and write back the same key. With neither key they write `harnesses:`. When the user list is absent, an edit starts from the effective inherited list, so later organization additions are no longer inherited until the key is deleted.
4. **Last-target removal.** Saving an empty list writes `targets: []` only when the file already authored `targets:`. Otherwise both the CLI editor and the TUI refuse without changing config or lock and show guidance to upgrade every reader of the file, rename the key to `targets:`, and retry. Writing `harnesses: []` or deleting the key would restore inheritance and is never done to represent an empty selection.
5. **Groups.** A group `targets:` list behaves like a group `harnesses:` list: it narrows and never adds a target. No new widening refusal is introduced.
6. **Compatibility proof.** Pinned fixtures captured from the current reader prove that absent lists, `harnesses: []` and non-empty `harnesses:` resolve identically through `sync` and `status` after this change. No released binary is executed.

## Risks

- A committed repository file using `targets:` breaks teammates on older clients. The release note and refusal guidance state the upgrade requirement; editors never introduce `targets:`.
- Snapshotting an inherited list stops future organization additions from reaching that user; this is the agreed trade-off and deleting the key restores inheritance.
