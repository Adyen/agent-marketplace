## MODIFIED Requirements

### Requirement: Deployed evidence is validated before use

The CLI MUST reject malformed, wrong-scope, or stale deployed organization acquisition locks before using their remote resources, while permitting organization deployments without a lock where online acquisition is allowed. The explicit organization lock write (`lock --scope organization`, with or without `--upgrade`) is the only exception for staleness: it MAY reconcile a stale but otherwise valid organization lock as defined by the explicit organization relock requirement. Single-file configuration reads MUST reject acquisition locks declaring a different layer scope.

#### Scenario: Absent organization lock

- **WHEN** an organization configuration has no deployed acquisition lock
- **THEN** inspection of deployed locks MUST leave that layer without evidence rather than inventing a deployed lock

#### Scenario: Stale organization lock

- **WHEN** a deployed organization acquisition lock is stale relative to its configuration
- **THEN** the CLI MUST reject that lock rather than silently acquiring replacement authority

#### Scenario: Malformed organization lock

- **WHEN** a deployed organization acquisition lock is malformed
- **THEN** the CLI MUST reject it rather than use or replace its contents

#### Scenario: Wrong scope in single-file read

- **WHEN** a single-file loader reads an otherwise valid acquisition lock declaring a different layer scope
- **THEN** it MUST reject the lock without binding its remote evidence

## ADDED Requirements

### Requirement: Explicit organization lock relocks changed sources

When the organization configuration changes a Git source or `ref` after its organization acquisition lock was written, `lock --scope organization` SHALL reconcile the existing lock instead of refusing it as stale. Plain `lock --scope organization` SHALL resolve only the Git entries whose source or `ref` is not recorded in the lock, SHALL keep every still-configured recorded entry byte-identical at its locked commit, and SHALL drop recorded entries that are no longer configured. `lock --scope organization --upgrade` SHALL resolve every configured entry at its current ref. Neither form SHALL require deleting the organization lock. Every other command and mode MUST keep refusing a stale organization lock with `lock.stale`, and the explicit write MUST still refuse a malformed, wrong-scope, or untrusted organization lock without replacing it.

#### Scenario: Plain organization lock relocks a changed ref

- **WHEN** an organization lock records two Git sources and the organization configuration changes the `ref` of one of them
- **THEN** `lock --scope organization` MUST succeed and record the changed source at the commit of its new ref
- **AND** the unchanged source's lock record MUST stay byte-identical, even when its ref has moved upstream
- **AND** no lock file deletion MUST be needed

#### Scenario: Organization lock upgrade after a ref change moves every entry

- **WHEN** an organization lock records two Git sources, the organization configuration changes the `ref` of one of them, and the other source's ref has moved upstream
- **THEN** `lock --scope organization --upgrade` MUST succeed and record both sources at the current commits of their configured refs

#### Scenario: Other commands keep refusing a stale organization lock

- **WHEN** the organization configuration changes a Git source's `ref` and the organization lock is not relocked
- **THEN** `sync`, `sync --locked`, `status`, `lock --check`, and `lock` without `--scope organization` MUST fail with `lock.stale`
- **AND** they MUST NOT write the organization lock

#### Scenario: Explicit organization lock refuses invalid lock evidence

- **WHEN** the organization lock is malformed, declares a different scope, or fails organization trust validation, and the organization configuration has changed a Git source's `ref`
- **THEN** `lock --scope organization` MUST fail without replacing the organization lock

#### Scenario: Failed re-resolution leaves the organization lock unchanged

- **WHEN** the organization configuration changes a Git source's `ref` to a ref that cannot be resolved
- **THEN** `lock --scope organization` MUST fail
- **AND** the organization lock bytes and file mode MUST stay unchanged
