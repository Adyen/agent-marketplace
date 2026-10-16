## Why

When a file-mode copy that Agent Marketplace wrote and recorded in the materialization manifest is edited locally, sync preserves the edit but warns that the output is "not managed by agent-marketplace" and suggests excluding the resource. The output is managed; the user changed it. The wrong diagnosis sends users to the wrong fix and hides that registry updates are no longer applied to that copy.

## What Changes

- An edited file-mode copy that the materialization manifest records with a digest is reported as a locally edited copy of a managed resource, naming the path and resource and explaining how to restore the registry version.
- The same message applies when such a copy blocks a harness's return from file mode to cache-backed symlinks.
- The diagnostic code (`materialize.collision.user-owned`) and severity (warning), the preservation of the edited bytes, and the exit code are unchanged.
- Output that the manifest does not record keeps the existing unmanaged-collision warning and remove/rename hint.

## Capabilities

### New Capabilities

### Modified Capabilities

- `materialization`: adds a requirement for how locally edited file-mode output recorded in the materialization manifest is reported.

## Impact

File-mode collision diagnostics in the materialization pipeline and their tests. No configuration, lockfile, manifest, or ownership-decision change.
