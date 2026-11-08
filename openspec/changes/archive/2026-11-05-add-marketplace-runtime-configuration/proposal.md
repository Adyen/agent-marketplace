# Proposal

## Why

The Marketplace worker reads its Marketplace operator configuration once at startup, so adding a registry, changing a tracked ref, or changing indexing policy has no effect until the process restarts, and the refresh decision compares a byte-level digest of startup values that over-triggers after irrelevant edits. Source observation and conditional refreshes from `marketplace-source-observation` now exist, so the worker can apply worker-safe configuration changes while it runs and refresh only the registries whose indexed result may change.

## What Changes

- `serve` (worker and all roles) polls the content of its configuration file about once per second. Unchanged bytes cause no parsing and no registry work. Changed bytes are parsed and validated as a complete candidate; a missing, unreadable, partially written, malformed, or invalid candidate is rejected and logged, and the complete last known-good runtime configuration stays in effect. No filesystem notification or signal is required.
- Each registry's indexing generation becomes a versioned digest over an explicit canonical projection of the values that can change its indexed output: origin, ref, registry roots and source harnesses in authored order, resolved acquisition mode, license, credential and CA references (never secret values), policy overrides, and the applicable global policy and limits. Comments, formatting, mapping order, and equivalent default spellings produce the same generation.
- A valid candidate is classified against the running configuration. Registry additions are recorded and observed immediately; a changed indexing generation makes that registry's new generation desired and refreshes it once, even on an unchanged commit; slug, alias, and publisher changes update registry metadata without acquisition; a removed registry becomes unlisted, stops being observed, and keeps its immutable history. The registry and desired-state writes for one candidate commit atomically in SQLite and PostgreSQL; if they fail, nothing is applied and the previous runtime configuration stays in effect until the candidate is retried.
- Changes to the listener, operations listener, storage, Marketplace instance identity, revocations, management, administration, catalog projection, installation metrics, and refresh concurrency are reported as requiring a restart and are not applied; the report clears when the file matches the running startup values again. Revocations are not hot-reloaded.
- Publication of a refresh requires that its indexing generation is still the desired generation. A refresh started for an older generation is discarded without publishing, without counting as a refresh failure, and the current desired work is scheduled.

## Capabilities

### New Capabilities

- `marketplace-runtime-configuration`: runtime observation of the Marketplace operator configuration by `serve`, covering content polling, last known-good retention, semantic classification, restart-required reporting, and registry add, change, and remove reconciliation.

### Modified Capabilities

- `marketplace-source-observation`: refresh triggers compare the canonical indexing generation of the running configuration instead of the startup configuration, and a stale generation cannot publish.
- `marketplace-registry-observation`: the metadata store applies a configuration candidate's registry and desired-state changes atomically and rejects publication whose generation is no longer desired, in both SQLite and PostgreSQL.

## Impact

- Marketplace operator-config management mode: `serve` wiring, the worker's observer and refresh processing, the operator configuration package (parsing from bytes, canonical projection, classification), and metadata-store publication and reconciliation in both backends.
- No metadata schema migration and no new operator configuration field. Database management mode, the API role, and `serve reindex` keep using the configuration they loaded at startup.
- After upgrade every registry's recorded generation differs from the new canonical generation, so each registry refreshes once.
- Operator documentation describes which configuration changes apply without a restart.

## Release note

The Marketplace worker now notices edits to its operator configuration file without a restart: added, changed, and removed registries and indexing-policy changes apply within seconds and refresh only the affected registries, while invalid edits are rejected and the last good configuration keeps running. Changes to listeners, storage, instance identity, or revocations are logged as requiring a restart. After upgrading, each registry reindexes once.
