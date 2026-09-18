# How to migrate configuration

Repair a configuration that `agent-marketplace` rejects for structural reasons, using `agent-marketplace migrate` where it can help and hand edits where it cannot.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- `agent-marketplace status` or `sync` reports a configuration validation error.

## What `migrate` does and does not do

`migrate` runs one transactional pass over `~/.agents/config.yaml`, `.agents/config.yaml`, and `.agents/config.local.yaml`. Every file is validated before any file is written, and every modified file is written atomically. It:

| Remediation | Layer | Behavior |
|---|---|---|
| Adds a `harnesses` list to the user config | user | Uses the existing user list, else `--harnesses`, else prompts on a terminal |
| Rewrites legacy `collections` sequences (`- name: x` or `- x`) to `collections: {include: [x]}` | all | Comments are preserved |
| Removes `backup:` and `logging:` | repo | These keys are user-level only |
| Removes `harnesses:`, `backup:`, `logging:`, `allowed_registries:`, `allowed_marketplaces:` | local | These keys are not allowed in the personal-local layer |

A file that is already valid v2 or v3 is left byte-for-byte unchanged. `migrate` never adds `devin` or `codex` to a harness list on its own.

It refuses, without modifying anything:

- any existing file with **no `apiVersion`** (`migrate` does not translate v1 configs, and requires an explicit `apiVersion` on every existing file it inspects, including local configs where it is otherwise optional);
- a file containing the legacy `identity` or `team_profiles` keys;
- an `apiVersion` other than `agent-marketplace/v2` or `agent-marketplace/v3`.

## 1. Identify the problem

```bash
agent-marketplace status
```

The `ERRORS` section names the file and the offending key or line. Decide from the table above whether `migrate` covers it.

## 2. Supply a missing apiVersion or convert a v1 file by hand

`migrate` requires every existing file it inspects to declare an accepted `apiVersion` (`agent-marketplace/v2` or `agent-marketplace/v3`). If a file (including a local `.agents/config.local.yaml` that omitted it during normal loading) lacks `apiVersion`, add `apiVersion: agent-marketplace/v2` to it before running `migrate`.

A legacy v1 file has no `apiVersion` and may contain obsolete keys. Before `migrate` can touch it, edit it into a valid v2 root:

Before:

```yaml
# .agents/config.yaml (v1)
registries:
  - url: https://github.com/example-org/infra-skills.git
    ref: main
    skills:
      - vm-lifecycle
collections:
  - name: platform-onboarding
identity:
  team: platform
```

After:

```yaml
# .agents/config.yaml (v2)
apiVersion: agent-marketplace/v2
harnesses:
  - claude
registries:
  - url: https://github.com/example-org/infra-skills.git
    ref: main
    skills:
      include:
        - vm-lifecycle
collections:
  include:
    - platform-onboarding
```

The changes: add `apiVersion`; declare `harnesses` (repo-level is optional if your user config has one); use the map form of selectors (`include:`), although the bare list is still accepted; drop `identity` and `team_profiles`, which have no v2 equivalent.

## 3. Run the migration

```bash
agent-marketplace migrate --harnesses claude,factory
```

`--harnesses` is only used when the user config needs a list and has none; an existing user list wins. On a terminal without `--harnesses`, the command prompts. The output names each rewritten file, or:

```text
No structural remediation needed; config files are already valid
```

Then sync:

```bash
agent-marketplace sync
```

## 4. Verify

```bash
agent-marketplace status
```

`ERRORS` should be empty and `TARGET HARNESS SET` should show the intended harnesses and their source layer.

## Troubleshooting

- **`has no apiVersion (v1 input is not supported)`**: Add `apiVersion: agent-marketplace/v2` to the file (including any `.agents/config.local.yaml` that omitted it) or convert the file by hand as in step 2, then rerun `migrate`.
- **`config contains legacy identity or team_profiles fields`**: Delete those keys; nothing in v2 replaces them.
- **`target harness set is required`**: The user config exists but has no `harnesses`; pass `--harnesses` or run on a terminal to be prompted.
- **A local config still contains `harnesses`**: `migrate` removes it, because the target set is machine-wide and lives in the user or repository config.

## Related documentation

- [Repository configuration reference](../reference/configuration.md)
- [Configuration layers reference](../reference/configuration-layers.md)
