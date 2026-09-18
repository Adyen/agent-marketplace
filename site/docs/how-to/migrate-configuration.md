# How to migrate configuration

Convert representable legacy configuration to the v4 source/resource schema while keeping v2/v3 reads available. For legacy structural remediation without a v4 conversion, use `agent-marketplace migrate --to v2`.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A writable v2 or v3 root that you want to convert to v4, or a v1 file that you have already converted into a valid v2 root.
- `agent-marketplace status` or `sync` reports a configuration validation error when structural remediation is needed.
- For a config containing exact Marketplace pins: verified local cache metadata — an assessed revision record plus the artifact or collection manifest for every pin. Normal legacy `sync` populates this evidence; migration performs no network access and refuses the file when any pin's evidence is missing.

## What `migrate` does and does not do

By default, `migrate` converts representable standalone v2/v3 roots to the canonical v4 source/resource schema. The conversion is semantic and selection-preserving, but does not promise authored comments or formatting. It is transactional: all selected inputs are validated before any file is written.

Preview all writable layers without writing:

```bash
agent-marketplace migrate --dry-run
```

V4 migration:

- targets user, repository, and repository-personal config layers; use `--scope user`, `--scope repo`, or `--scope local` to limit the transaction;
- refuses include trees, hosted Marketplace selections without proven pin provenance, and other lossy inputs without changing bytes;
- never edits organization configuration;
- leaves Git- and directory-only locks unchanged, while a proven Marketplace conversion creates or upgrades the owning layer lock to `agent-marketplace-lock/v2` atomically with the config in one journaled transaction;
- does not add `devin` or `codex` implicitly; and
- leaves v2/v3 configs readable when migration is declined or refused.

Interactive CLI startup and the TUI ask before migrating detected legacy roots. A declined or non-interactive prompt leaves the original files unchanged and continues with v2/v3 reads. Run the command below explicitly to publish a conversion:

```bash
agent-marketplace migrate
```

The `--to v4` spelling is accepted as an alias. The `--to v2` spelling selects the legacy structural-remediation flow described below.

Legacy structural remediation with `--to v2` runs one transactional pass over `~/.agents/config.yaml`, `.agents/config.yaml`, and `.agents/config.local.yaml`. It:

| Remediation | Layer | Behavior |
|---|---|---|
| Adds a `harnesses` list to the user config | user | Uses the existing user list, else `--harnesses`, else prompts on a terminal |
| Rewrites legacy `collections` sequences (`- name: x` or `- x`) to `collections: {include: [x]}` | all | Comments are preserved |
| Removes `backup:` and `logging:` | repo | These keys are user-level only |
| Removes `harnesses:`, `backup:`, `logging:`, `allowed_registries:`, `allowed_marketplaces:` | local | These keys are not allowed in the personal-local layer |

A file that is already valid v2 or v3 is left byte-for-byte unchanged by the structural-remediation flow. It never adds `devin` or `codex` to a harness list on its own. Use it as follows when a configuration is rejected for structural reasons:

```bash
agent-marketplace migrate --to v2 --harnesses claude,factory
```

`--harnesses` is only used when the user config needs a list and has none; an existing user list wins. On a terminal without `--harnesses`, the command prompts. A non-interactive invocation without a usable target harness set fails rather than guessing.

## 1. Identify the problem

```bash
agent-marketplace status
```

The `ERRORS` section names the file and the offending key or line. Decide from the boundaries above whether v4 migration or structural remediation covers it.

## 2. Supply a missing apiVersion or convert a v1 file by hand

V4 migration requires an accepted `apiVersion` on regular user and repository roots. A local root may omit it during normal loading, but an unversioned local file is considered a legacy migration candidate. A legacy v1 file has no `apiVersion` and may contain obsolete keys; convert it into a valid v2 root before using the v4 migrator:

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

The changes are: add `apiVersion`; declare `harnesses` (repository-level harnesses are optional if your user config has them); use the map form of selectors (`include:`), although the bare list is still accepted; and drop `identity` and `team_profiles`, which have no v2 equivalent.

## 3. Review and run the migration

Then sync:

```bash
agent-marketplace sync
```

## 4. Verify

Inspect the migrated root and confirm it declares v4 sources and resources:

```yaml
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: https://github.com/example-org/infra-skills.git
    ref: main
resources:
  infra:
    include: [skill/vm-lifecycle]
```

When the legacy config carried proven Marketplace pins, also inspect the adjacent layer lock (for example `.agents/lock.yaml` next to `.agents/config.yaml`). It declares `apiVersion: agent-marketplace-lock/v2` with the migrated endpoint, registry bindings, resource digests, and selection edges that back the v4 `marketplace` source.

For structural remediation, verify the result with:

```bash
agent-marketplace status
```

`ERRORS` should be empty and `TARGET HARNESS SET` should show the intended harnesses and their source layer.

## Boundaries

- `migrate` targets v4 by default; `--to v4` remains an accepted alias.
- It is semantic and selection-preserving for representable standalone roots and transactional across the selected writable layers.
- It does not add `devin` or `codex` implicitly and never edits organization config. Git/directory-only conversion leaves locks unchanged; a proven Marketplace conversion creates or upgrades the owning layer lock to `agent-marketplace-lock/v2` atomically with the config in one journaled transaction.
- It refuses `include` trees, hosted Marketplace selections without proven pin provenance, collection-derived aliases that cannot be represented, and other lossy inputs without changing bytes.
- `--to v2 --harnesses <targets>` remains available for legacy structural remediation.
- v1 roots are rejected and must be converted to valid v2 first.

## Troubleshooting

- **Migration refuses a file**: The file may be v1, contain unsupported include provenance, contain Marketplace pins whose verified cache evidence is missing or inconsistent, or contain legacy `identity` or `team_profiles` fields. Convert v1 by hand or resolve the unsupported fields before migrating.
- **`v4.migration-marketplace-evidence`**: A Marketplace pin's verified cache evidence is missing or inconsistent. Run a normal legacy `sync` so the CLI fetches and verifies the endpoint's metadata/artifacts, then rerun `migrate` — do not hand-author lock or trust data.
- **`has no apiVersion (v1 input is not supported)`**: Add `apiVersion: agent-marketplace/v2` to a regular file or convert the file by hand as in step 2. A local file that intentionally omitted the version can be migrated through the v4 flow, but regular roots must declare one.
- **The prompt is skipped**: Run `agent-marketplace migrate` explicitly on a terminal. Non-interactive commands keep using v2/v3 and never rewrite without consent.
- **`target harness set is required`**: The v2 structural-remediation flow needs an existing user harness list, `--harnesses`, or an interactive prompt.
- **A local config still contains `harnesses`**: The v2 structural-remediation flow removes it, because the target set is machine-wide and lives in the user or repository config.

## Related documentation

- [Repository configuration reference](../reference/configuration.md)
- [Configuration layers reference](../reference/configuration-layers.md)
