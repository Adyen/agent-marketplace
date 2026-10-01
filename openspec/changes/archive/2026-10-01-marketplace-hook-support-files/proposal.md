## Why

Issue #449: mirror hook artifacts omit adjacent executable assets. Migrating
from Git prunes the scripts while retaining a hook command that references them.

## What Changes

- Publish hook support files using the same source-layout exclusions as Git
  materialization, and bind their bytes and executable modes into new revisions.
- Verify and stage mirrored hook support files before native materialization.
- Refuse legacy script-dependent mirror hooks without packaged support instead
  of reporting a healthy installation. Existing pinned archives stay immutable.

## Impact

Marketplace ingestion, canonical hook payloads, mirror consumption, and hook
materialization. No changes to organization policy or user configuration.
