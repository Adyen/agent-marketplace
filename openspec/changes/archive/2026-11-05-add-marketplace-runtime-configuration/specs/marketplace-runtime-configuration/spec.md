# Spec Delta

## Purpose

`serve` observes its Marketplace operator configuration file while it runs, applies worker-safe changes to registries and indexing policy without a restart, keeps the last known-good configuration when a replacement is invalid, and reports changes that only a restart can apply.

## ADDED Requirements

### Requirement: The runtime configuration is observed by content polling

When `serve` runs the worker role (`--role worker` or `--role all`) in operator-config management mode, it SHALL read the Marketplace operator configuration file at its configured path about once per second and compare the bytes with the last bytes it read. Unchanged bytes MUST cause no parsing, no metadata-store write, no refresh request, and no new runtime configuration version. Changed bytes SHALL be parsed and validated as one complete candidate. Each applied candidate SHALL become a new immutable runtime configuration version that the worker uses for its next observations and refreshes. Observation MUST NOT depend on filesystem notifications or signals, and it SHALL notice a file replaced by renaming another file over it. Observation SHALL stop when `serve` stops.

#### Scenario: Unchanged configuration bytes cause no work

- **WHEN** the configuration file is polled repeatedly without its bytes changing
- **THEN** no candidate is parsed, no registry or observation record is written, no refresh is requested, and the runtime configuration version stays the same

#### Scenario: A replaced configuration file is applied

- **WHEN** the configuration file is atomically replaced by renaming a file with a valid changed configuration over it
- **THEN** the next poll applies the candidate and the worker's runtime configuration version advances to it

### Requirement: Formatting-only edits cause no registry work

A candidate whose worker-relevant meaning equals the running configuration SHALL cause no registry work: changed comments, whitespace, mapping order, or an explicitly written value equal to its default MUST NOT write registry or observation records, request a refresh, or acquire any registry.

#### Scenario: A comment and formatting edit causes no registry work

- **WHEN** the configuration file is rewritten with added comments, reordered mapping keys, different indentation, and a registry's acquisition mode written explicitly with its default value
- **THEN** no registry or observation record is written, no refresh is requested, and no registry is acquired

### Requirement: Invalid replacements keep the last known-good configuration

A candidate that is missing, unreadable, empty, partially written, malformed, or fails operator-configuration validation SHALL be rejected as a whole. The worker MUST keep using the complete last known-good runtime configuration, MUST NOT treat a missing or empty file as an empty registry list, and MUST NOT unlist, add, or refresh any registry because of the rejected candidate. The rejection SHALL be logged once per distinct rejected content with a bounded reason that contains no credential value. A later valid candidate SHALL be applied normally.

#### Scenario: A partially written configuration is rejected

- **WHEN** the configuration file is replaced by a truncated copy that is not valid YAML, and later by a complete valid configuration that adds a registry
- **THEN** the truncated candidate is rejected and logged, every registry stays listed and unchanged while it is present, and the later valid candidate adds the registry

#### Scenario: A missing configuration file keeps every registry

- **WHEN** the configuration file is deleted while the worker runs
- **THEN** the rejection is logged, the runtime configuration version stays the same, and no registry is unlisted or refreshed

#### Scenario: A configuration that fails validation is rejected

- **WHEN** the configuration file is replaced by well-formed YAML that declares two registries with the same ID
- **THEN** the candidate is rejected and logged and the last known-good runtime configuration stays in effect

### Requirement: Configuration changes are classified by meaning

The worker SHALL classify a valid candidate by comparing its canonical meaning with the running configuration. Each registry's indexing generation SHALL be a versioned digest of an explicit projection containing only its origin, ref, registry roots and source harnesses in authored order, resolved acquisition mode, license, credential and CA references, policy overrides, and the applicable global policy and limits; it MUST NOT contain a resolved secret value. A changed indexing generation SHALL be applied without a restart. A changed `refresh.interval` SHALL take effect at each registry's next scheduled observation and MUST NOT by itself request a refresh. Changes to `listen`, `operations_listen`, `storage`, `instance`, `revocations`, `management`, `admin`, `catalog_projection`, `installation_metrics`, or `refresh_concurrency` SHALL be reported as requiring a restart and MUST NOT be applied: the running listener, stores, Marketplace identity, and active revocations stay unchanged while the rest of the candidate applies. The restart-required report SHALL be logged with the names of the changed fields when it appears or changes, and SHALL be logged as cleared once the file's values for those fields match the running startup values again.

#### Scenario: A worker-safe change applies while restart-required changes are reported

- **WHEN** one candidate changes a registry's policy overrides and also changes the listen address, the metadata storage settings, the Marketplace instance name, and the revocations
- **THEN** the registry's new indexing generation becomes desired, the restart-required report names `listen`, `storage`, `instance`, and `revocations`, and the running listener, stores, Marketplace record, and active revocations are unchanged

#### Scenario: Restoring startup values clears the restart-required report

- **WHEN** a candidate that changed the listen address is followed by a candidate whose listen address matches the running startup value
- **THEN** the worker logs that the restart-required report is cleared

#### Scenario: A refresh interval change requests no refresh

- **WHEN** a candidate changes only `refresh.interval` for successfully indexed registries
- **THEN** no refresh is requested and each registry's next observation after its pending one is scheduled within the new interval

#### Scenario: The indexing generation excludes secret values

- **WHEN** two candidates differ only in the content of a registry's token file or token environment variable value while the token reference is unchanged
- **THEN** both candidates produce the same indexing generation for that registry

### Requirement: Registry additions, changes, and removals are reconciled

When a valid candidate is applied, the worker SHALL record each added registry and observe it immediately, so it is indexed once like any never-indexed registry. A registry whose indexing generation changed SHALL have its new generation made desired and SHALL be observed immediately; when its commit is unchanged, exactly one refresh SHALL run for the new generation. A registry whose only changes are its slug, aliases, or publisher SHALL have its registry metadata updated without acquisition, preserving prior slugs as aliases. A removed registry SHALL become unlisted, SHALL no longer be observed or refreshed, and SHALL keep its registry ID, aliases, registry occurrences, resource revisions, resource occurrences, resource assessments, and blobs. A removed registry that is added again SHALL become listed again.

#### Scenario: An added registry is observed and indexed

- **WHEN** a candidate adds a registry tracking a branch of a local fixture repository
- **THEN** the registry is recorded as listed, observed without waiting for its phase, and indexed exactly once

#### Scenario: A same-commit indexing change refreshes once

- **WHEN** a candidate changes the policy overrides of a successfully indexed registry whose tracked commit is unchanged, and the registry is then observed several times
- **THEN** exactly one refresh runs for the new indexing generation, it records that generation as successful, and later observations perform no acquisition

#### Scenario: A presentation-only change updates metadata without acquisition

- **WHEN** a candidate changes only a registry's slug and publisher name
- **THEN** the registry record shows the new slug with the prior slug as an alias and the new publisher name, and no refresh is requested

#### Scenario: A removed registry is unlisted and keeps its history

- **WHEN** a candidate removes a successfully indexed registry
- **THEN** the registry becomes unlisted, it is no longer observed or refreshed, and its registry occurrences, resource revisions, resource occurrences, resource assessments, and blobs remain

### Requirement: A stale indexing generation cannot publish

A refresh SHALL publish only while its indexing generation is the registry's desired generation. The worker SHALL check the desired generation before acquisition, and publication itself SHALL atomically reject a generation that is no longer desired. A refresh whose generation is no longer desired MUST NOT change the registry head, resources, or refresh status, MUST NOT count as a failed refresh attempt, SHALL be logged as stale, and SHALL leave the registry due so its current desired generation is refreshed. A refresh whose generation is desired but not yet known to the worker's runtime configuration SHALL be returned to the queue without consuming an attempt.

#### Scenario: A refresh started for an older generation cannot publish

- **WHEN** a refresh for one indexing generation is acquiring a registry, a candidate that changes that registry's policy overrides is applied before the refresh publishes, and the refresh then finishes ingestion
- **THEN** the older refresh publishes nothing and leaves the registry head and refresh status unchanged, a stale-generation record is logged, and a refresh for the new generation then publishes and records the new generation as successful
