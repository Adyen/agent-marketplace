## ADDED Requirements

### Requirement: Missing exact snapshot recovery feedback

During TUI startup and reload, an acquisition lock whose exact snapshot is absent from the local cache SHALL produce a warning retaining `lock.snapshot-unavailable` context and instructing the user to press `s` to sync or run `agent-marketplace sync --locked`. The affected selections and drift SHALL remain unverified, never presented as in sync or as a successful authoritative reload. Inspection SHALL NOT acquire the missing exact snapshot, substitute mutable-ref content, or write configuration, acquisition locks, ownership state, or materialized output. Explicit synchronization SHALL remain available through the normal guarded lifecycle. This presentation policy SHALL NOT downgrade untrusted or mismatched exact snapshots, failed synchronization, or CLI check and offline failures.

#### Scenario: Missing locked snapshot is a recoverable warning

- **GIVEN** an acquisition lock records an exact commit not cached by the consumer
- **WHEN** the TUI starts or reloads
- **THEN** it SHALL show a missing-snapshot warning with the synchronization instructions while retaining unverified selections and drift
- **AND** inspection SHALL leave configuration, acquisition locks, the affected source's exact cache, and materialized output unchanged without acquiring or substituting its content

#### Scenario: Sync recovery preserves the acquisition lock

- **GIVEN** an incoming acquisition lock records an uncached commit and its mutable ref has since advanced again
- **WHEN** the consumer explicitly syncs
- **THEN** synchronization SHALL acquire and materialize the locked commit, retain byte-identical acquisition locks, and permit subsequent exact inspection
- **AND** CLI check commands SHALL fail for the missing exact snapshot before synchronization

#### Scenario: Untrusted snapshots remain errors

- **WHEN** the TUI inspects an exact snapshot whose tracked contents no longer match its locked commit
- **THEN** the snapshot SHALL remain an error rather than a recoverable cache-miss warning and drift SHALL remain unverified

#### Scenario: Failed sync remains an error

- **GIVEN** the TUI displays a recoverable missing-snapshot warning
- **WHEN** explicit synchronization fails and inspection still cannot verify the exact snapshot
- **THEN** the synchronization failure SHALL remain an error alongside the cache-miss warning, without an in-sync or synchronization-success claim

### Requirement: Discoverable missing-snapshot recovery shortcut

When the TUI displays a recoverable missing-exact-snapshot warning without unrelated inspection or operation errors, the resource-list footer SHALL prioritize the `s` synchronization shortcut at every supported terminal width while retaining the command palette, help, and quit entry points. The shortcut SHALL use the ordinary synchronization guards and SHALL NOT imply that inspection has verified the missing exact snapshot.

#### Scenario: Sync recovery shortcut remains visible

- **WHEN** the TUI displays a missing-exact-snapshot warning without unrelated errors at 50, 80, or 120 columns
- **THEN** the resource-list footer SHALL show the sync shortcut alongside command palette, help, and quit hints without duplicated sync hints
- **AND** invoking sync SHALL acquire the locked content and replace the warning with the verified post-sync state
