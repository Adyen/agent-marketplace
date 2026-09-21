# Maintain v4 configuration

Keep a v4 configuration readable, reproducible, and safe to change across its layered files and generated locks. Representable legacy roots can be converted to v4 while v2/v3 reads remain available; use `agent-marketplace migrate --to v2` when only legacy structural remediation is needed.

::: warning Migration changes the editing workflow
Discovery, status, and sync can read v4. Commands that install or remove resources, endpoint registration, and TUI resource toggles still use legacy editors, which reject v4 files with `v4.writer-opt-in`. Keep v2/v3 if you need these editing commands. This is why `init` creates an editable v2 user config and migration requires an explicit command.

For v4 Git/directory selections, edit `sources` and `resources` directly, then sync. Exact hosted requests need matching verified records in the layer's lockfile. General hosted acquisition, update, and snapshots for dynamic selectors are not implemented. You do not need to migrate to follow an install command.
:::

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A root configuration at `.agents/config.yaml` or `~/.agents/config.yaml`.
- A writable v2 or v3 root that you want to convert to v4, or a v1 file that you have already converted into a valid v2 root.
- For a config containing exact Marketplace pins: verified local cache metadata, including an assessed revision record and the artifact or collection manifest for every pin. Normal legacy `sync` populates this evidence. Migration makes no network requests and refuses the file if any pin's evidence is missing.

## 1. Inspect the current configuration

Open the root and confirm that it declares the v4 schema:

```yaml
apiVersion: agent-marketplace/v4
```

The root's `sources` map declares Git, directory, or Marketplace inputs. Its `resources` map selects items from those sources:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: main
resources:
  shared:
    include: [skill/code-review]
```

## 2. Migrate a legacy configuration

By default, `migrate` converts representable standalone v2/v3 roots to the canonical v4 source/resource schema. The conversion is semantic and selection-preserving, but does not promise authored comments or formatting. It is transactional: all selected inputs are validated before any file is written.

Preview all writable layers without writing:

```bash
agent-marketplace migrate --dry-run
```

V4 migration targets user, repository, and repository-personal config layers; use `--scope user`, `--scope repo`, or `--scope local` to limit the transaction. It refuses include trees, hosted Marketplace selections without proven pin provenance, collection-derived aliases that cannot be represented, and other lossy inputs without changing bytes. It never edits organization configuration. Git- and directory-only conversion leaves locks unchanged; a proven Marketplace conversion creates or upgrades the owning layer lock to `agent-marketplace-lock/v2` atomically with the config in one journaled transaction.

The CLI and TUI leave legacy roots unchanged at startup while resource editors still require v2/v3. To convert them, run the command below; you can also pass `--to v4`:

```bash
agent-marketplace migrate
```

When the legacy config carried proven Marketplace pins, inspect the adjacent layer lock (for example `.agents/lock.yaml` next to `.agents/config.yaml`). It declares `apiVersion: agent-marketplace-lock/v2` with the migrated endpoint, registry bindings, resource digests, and selection edges that back the v4 `marketplace` source.

### Legacy structural remediation

Use `--to v2` when you need to repair a legacy configuration without converting it to v4. The flow runs one transactional pass over `~/.agents/config.yaml`, `.agents/config.yaml`, and `.agents/config.local.yaml`:

| Remediation | Layer | Behavior |
|---|---|---|
| Adds a `harnesses` list to the user config | user | Uses the existing user list, else `--harnesses`, else prompts on a terminal |
| Rewrites legacy `collections` sequences (`- name: x` or `- x`) to `collections: {include: [x]}` | all | Comments are preserved |
| Removes `backup:` and `logging:` | repo | These keys are user-level only |
| Removes `harnesses:`, `backup:`, `logging:`, `allowed_registries:`, `allowed_marketplaces:` | local | These keys are not allowed in the personal-local layer |

A file that is already valid v2 or v3 is left byte-for-byte unchanged by this flow. It never adds `devin` or `codex` to a harness list on its own:

```bash
agent-marketplace migrate --to v2 --harnesses claude,factory
```

`--harnesses` is only used when the user config needs a list and has none; an existing user list wins. On a terminal without `--harnesses`, the command prompts. A non-interactive invocation without a usable target harness set fails rather than guessing.

### Convert v1 by hand

V4 migration requires an accepted `apiVersion` on regular user and repository roots. A local root may omit it during normal loading, but an unversioned local file is considered a legacy migration candidate. A legacy v1 file has no `apiVersion` and may contain obsolete keys; convert it into a valid v2 root before using the v4 migrator:

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

Add `apiVersion`, declare `harnesses` when needed, use the map form of selectors (`include:`), and drop `identity` and `team_profiles`, which have no v2 equivalent.

## 3. Change sources and resource selection

Declare exactly one acquisition kind for each named source. Use `directory` for live local content, even when the directory name ends in `.git`:

```yaml
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
  local:
    directory: ./local-resources
```

Git sources require `ref`. Directory sources in repository `.agents/config.yaml` and `.agents/config.local.yaml` roots resolve relative to the repository root; user/organization configs and included fragments retain declaring-file-relative paths. Directory sources never use a Git lock. Migration preserves repository-relative local paths and folds a legacy `path` into `directory`: `url: .` with `path: skills` becomes `directory: ./skills`, without embedding the checkout's absolute path. Bind resource policy to the source name:

```yaml
resources:
  shared:
    include:
      - skill/code-review
      - command/release
    exclude:
      - skill/obsolete-review
    harnesses: [claude, cursor]
  local:
    all: true
```

Use `tags` or `all` for dynamic selection. A Marketplace group using dynamic selection must also name the applicable `registries`. Use `required: true` to protect selected resources; it does not select resources by itself.

## 4. Validate without changing files

Check the exact Git locks and generated harness output before publishing a change:

```bash
agent-marketplace lock --check
agent-marketplace sync --check
```

Both checks are read-only. A non-zero result identifies lock or materialization drift that must be reconciled before the configuration is considered reproducible.

For structural remediation, `agent-marketplace status` should show no `ERRORS` and the intended `TARGET HARNESS SET`.

## 5. Publish the change

Commit the v4 configuration and repository lock together:

```bash
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Update agent resource selection"
```

Do not commit generated harness output. Keep `.agents/config.local.yaml` and `.agents/lock.local.yaml` private when using checkout-only overrides.

## Includes

Use `include` for reusable local fragments:

```yaml
apiVersion: agent-marketplace/v4
include:
  - profiles/company.yaml
  - profiles/team.yaml
```

Included fragments may omit `apiVersion`; they inherit v4 from the root. They are merged in order, and the containing file has the highest precedence. The loader rejects missing, malformed, unreadable, or cyclic includes.

## Troubleshooting

- `v4.api-version-required`: Add `apiVersion: agent-marketplace/v4` to the root.
- `v4.source-discriminator`: Keep exactly one of `git`, `directory`, or `marketplace` in each source declaration.
- `v4.runtime-option`: Remove unsupported options such as a non-default `as` alias for a Git or directory resource.
- `lock.snapshot-unavailable`: Restore the exact Git snapshot or run a writing lock operation before retrying the check.
- `v4.migration-marketplace-evidence`: A Marketplace pin's verified cache evidence is missing or inconsistent. Run a normal legacy `sync` to fetch and verify the endpoint's metadata and artifacts, then rerun `migrate`. Do not write lock or trust data by hand.
- No startup migration prompt: The CLI leaves v2/v3 files alone while resource editors still need them. Run `agent-marketplace migrate --dry-run` to preview a conversion before deciding to migrate.
- A local config still contains `harnesses`: The v2 structural-remediation flow removes it, because the target set is machine-wide and lives in the user or repository config.

See the [repository configuration reference](../reference/configuration.md) for the complete field and precedence rules.
