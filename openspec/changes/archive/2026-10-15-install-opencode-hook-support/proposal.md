## Why

OpenCode hooks are per-resource declarations, not entries in a shared hook file, so they bypassed the support installation and hook review that every other harness target gets. Hooks whose commands use `${CLAUDE_PLUGIN_ROOT}` or a project-directory variable were refused, and command changes to OpenCode declarations were never listed for review.

## What Changes

- Install each OpenCode hook's support files in `hook-support/<source>/<hook>/` under the OpenCode root, with the same capture, conflict, refusal, and prune rules as aggregate hook targets. Repository-scope support is installed as portable regular copies in both materialization modes.
- The fixed adapter sets `AGENT_MARKETPLACE_PROJECT_DIR` for every handler. Repository-scope plugin-root references resolve through it to the installed support directory; machine-scope references use the absolute support path; project-directory references translate to it.
- List added, replaced, and removed OpenCode declarations in the sync hook review with `materialize.hook.review`. `sync --check` lists pending declaration changes with `materialize.hook.review-pending`, writes nothing, and exits nonzero.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `opencode-hooks`: installed hook support, the adapter project directory, and declaration review.

## Impact

OpenCode adapter, hook catalog, and native layout; materialization of hooks for targets whose hooks are resource files. Native layouts gain a partition-level `HookSupportDir`. Aggregate hook targets are unchanged.
