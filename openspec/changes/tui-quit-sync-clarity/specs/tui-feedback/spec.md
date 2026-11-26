## ADDED Requirements

### Requirement: Quit-sync confirmation lists syncable changes

When the user presses `q` with syncable drift (`Missing` or `Orphaned`), the confirmation modal SHALL list affected resources with `+` (install/update) and `-` (remove) indicators bounded to terminal height with `+ <n> more` overflow. Unfixable `Broken` diagnostics alone SHALL NOT trigger `Sync+quit?`.

#### Scenario: Quit confirmation lists pending installs and removals

- **WHEN** a workspace has missing and orphaned resources and the user presses `q`
- **THEN** the confirmation modal MUST list those resource names with install/remove indicators within bounded terminal dimensions (50×12, 80×24, 120×34)

#### Scenario: Broken-only drift does not loop on quit

- **WHEN** a workspace has only `Broken` diagnostics and zero `Missing` or `Orphaned` items and the user presses `q`
- **THEN** the TUI MUST NOT prompt `Sync+quit?`

### Requirement: Post-sync check prevents silent quit on unresolved drift

After `sync+quit` completes without a hard synchronization error, the TUI SHALL verify the authoritative post-sync snapshot before exiting and SHALL remain open with diagnostics if syncable drift still remains. Hard synchronization errors SHALL continue propagating with their original error identity after terminal restoration.

#### Scenario: Remaining drift after sync keeps TUI open with diagnostics

- **WHEN** the user confirms `sync+quit`, synchronization completes without a hard error, and the authoritative post-sync check still reports unresolved syncable drift
- **THEN** the TUI MUST stay open and surface the unresolved state rather than exiting silently
