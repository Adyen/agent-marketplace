# Maintain v4 configuration

This guide covers converting older configuration files to v4 and then keeping a v4 configuration valid and reproducible. V4 is the current schema, and `init` creates new user configs in it. The CLI still reads v2 and v3 files, so you can migrate when it suits you. If you only need to repair the structure of a v2 or v3 file, use `agent-marketplace migrate --to v2` instead (see [Legacy structural remediation](#legacy-structural-remediation)).

::: warning What changes after migration
Discovery, `status`, and `sync` all read v4. Named installs and removals, TUI resource toggles, Marketplace installs, and the TUI harness modal (`H`) all edit v4 files. If a command reports `v4.writer-opt-in`, it tried to use an older editor that only handles v2 and v3. Edit `sources` and `resources` directly in that case, then run `sync`.

A few Marketplace features are not implemented for any schema: offline acquisition when the organization lock is absent, rebinding a hosted pin, and snapshots for dynamic selectors.
:::

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A root configuration at `.agents/config.yaml`, `.agents/config.local.yaml`, or `~/.agents/config.yaml`.
- To convert to v4: a writable v2 or v3 root. A v1 file (one with no `apiVersion`) must first be converted to v2 by hand, as described in [Convert v1 by hand](#convert-v1-by-hand).
- If the config contains exact Marketplace pins: verified local cache data for every pin, including the assessed revision record and the artifact or collection manifest. A normal `sync` with the legacy config fills in this data. Migration makes no network requests and refuses the file if any pin's data is missing.

## 1. Inspect the current configuration

Open the root file and check its first line. A v4 file declares:

```yaml
apiVersion: agent-marketplace/v4
```

The `sources` map declares where resources come from: a Git repository, a local directory, or a Marketplace. The `resources` map, keyed by the same names, selects items from each source:

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

By default, `migrate` converts v2 and v3 roots to v4. It keeps the same resource selection, but it does not keep comments or formatting. It is transactional: it validates every selected file before it writes any of them.

Preview the change without writing anything:

```bash
agent-marketplace migrate --dry-run
```

The output lists each file that would change, for example `Would migrate user personal config: /home/you/.agents/config.yaml`. If nothing needs converting, it prints `No legacy config files require v4 migration`.

When you are ready, run the migration. `--to v4` is the default, so you can leave it out:

```bash
agent-marketplace migrate
```

Each converted file is reported as `Migrated <layer> config: <path>`. Before it replaces a file, the CLI saves the original next to it as `<file>.pre-v4.<timestamp>.bak`, readable only by you.

Things to know about the migration:

- It covers the user, repository, and repository-personal [configuration layers](../glossary.md#configuration-layer). Use `--scope user`, `--scope repo`, or `--scope local` to convert just one. The default is `--scope all`.
- It never edits the organization configuration.
- It refuses, without changing any file, when a root uses `include`, when a Marketplace pin has no verified cache data, when a Marketplace resource that a collection brought in has its own local name (`installed_name`), and when some other part of the file cannot be represented in v4.
- For Git and directory sources, it leaves lockfiles unchanged. For Marketplace pins, it creates or upgrades the lockfile for the same layer to `agent-marketplace-lock/v2` and writes it together with the config in one transaction.

When you start the CLI or the TUI interactively and your user config is still v2 or v3, it asks whether to migrate that file. The CLI prompt ends in `[Y/n]`, so pressing Enter migrates. It asks only about the user config. Repository and personal-local files stay as they are until you run `migrate` yourself. If you decline, or the run is non-interactive, the CLI keeps reading the old file.

If the legacy config had Marketplace pins, open the lockfile next to it afterward (for example `.agents/lock.yaml` next to `.agents/config.yaml`). It declares `apiVersion: agent-marketplace-lock/v2` and records the endpoint, registry bindings, resource digests, and selections behind the new `marketplace` source.

### Legacy structural remediation

Use `--to v2` to repair a v2 or v3 configuration without converting it to v4. The command makes one transactional pass over `~/.agents/config.yaml`, `.agents/config.yaml`, and `.agents/config.local.yaml`:

| Remediation | Layer | Behavior |
|---|---|---|
| Adds a `harnesses` list to the user config | user | Uses the existing user list, else `--harnesses`, else prompts on a terminal |
| Rewrites legacy `collections` sequences (`- name: x` or `- x`) to `collections: {include: [x]}` | all | Comments are preserved |
| Removes `backup:` and `logging:` | repo | These keys are user-level only |
| Removes `harnesses:`, `backup:`, `logging:`, `allowed_registries:`, `allowed_marketplaces:` | local | These keys are not allowed in the personal-local layer |

A file that is already valid v2 or v3 stays byte-for-byte unchanged. The command never adds `devin` or `codex` to a harness list on its own:

```bash
agent-marketplace migrate --to v2 --harnesses claude,factory
```

`--harnesses` is only used when the user config has no harness list. An existing list always wins. Without `--harnesses`, the command prompts on a terminal and fails in a non-interactive run instead of guessing.

### Convert v1 by hand

The CLI rejects a user or repository root that has no `apiVersion`, reporting that v1 input is not supported. Convert such a file to v2 yourself, then run `migrate`:

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

Add `apiVersion`, add `harnesses` if the file needs one, use the map form of selectors (`include:`), and remove `identity` and `team_profiles`, which have no v2 equivalent.

A personal-local file (`.agents/config.local.yaml`) may leave out `apiVersion` during normal loading, but `migrate` treats an unversioned local file as a legacy file to convert.

## 3. Change sources and resource selection

Give each source exactly one of `git`, `directory`, or `marketplace`. Use `directory` for live local content, even when the directory name ends in `.git`:

```yaml
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
  local:
    directory: ./local-resources
```

Git sources must set `ref`. In `.agents/config.yaml` and `.agents/config.local.yaml`, a relative `directory` path starts from the repository root. In the user and organization configs, and in included fragments, it starts from the folder of the file that declares it. Directory sources never use a Git lock.

Migration keeps repository-relative local paths relative and folds a legacy `path` into `directory`. For example, `url: .` with `path: skills` becomes `directory: ./skills`, without writing the checkout's absolute path into the file.

Select resources under the same name as the source:

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

Use `tags` or `all` to select resources dynamically. A Marketplace group that uses `tags` or `all` must also list its `registries`. `required: true` protects the selected resources from being excluded, but it does not select anything by itself.

## 4. Validate without changing files

Check the Git locks and the generated harness output before you publish a change:

```bash
agent-marketplace lock --check
agent-marketplace sync --check
```

Neither command writes anything. A non-zero exit means the lock or the generated output has drifted and needs reconciling before the configuration is reproducible.

After a structural remediation, `agent-marketplace status` should show no `ERRORS` table and the intended harnesses in the `TARGET HARNESS SET` table.

## 5. Publish the change

Commit the v4 configuration and the repository lock together:

```bash
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Update agent resource selection"
```

Do not commit generated harness output. If you use checkout-only overrides, keep `.agents/config.local.yaml` and `.agents/lock.local.yaml` out of Git.

## Includes

Use `include` to reuse local fragments:

```yaml
apiVersion: agent-marketplace/v4
include:
  - profiles/company.yaml
  - profiles/team.yaml
```

Included fragments may leave out `apiVersion` and inherit v4 from the root. They merge in the order listed, and the file that includes them takes precedence over all of them. The loader rejects includes that are missing, malformed, unreadable, or circular.

## Troubleshooting

- `v4.api-version-required`: Add `apiVersion: agent-marketplace/v4` to the root.
- `v4.source-discriminator`: Keep exactly one of `git`, `directory`, or `marketplace` in each source.
- `v4.git-ref-required`: Add a `ref` to the Git source.
- `v4.dynamic-unbounded`: A Marketplace group uses `all` or `tags` without a `registries` list. Add one.
- `v4.layer-field`: The file sets a field that its layer does not allow, for example `harnesses` in `.agents/config.local.yaml`, or `backup`, `logging`, or `telemetry` in a repository config. Move the field to a layer that allows it, usually the user config.
- `v4.runtime-option`: Remove the unsupported option, such as an `as` alias that differs from the resource name on a Git or directory resource.
- `v4.migration-unsupported`: Part of the file cannot be converted, for example a root that uses `include`. The message names the part. Edit the file by hand, or keep it on v2 or v3.
- `v4.migration-marketplace-evidence`: The verified cache data for a Marketplace pin is missing or inconsistent. Run a normal `sync` to fetch and verify the endpoint's metadata and artifacts, then run `migrate` again. Do not write lock or trust data by hand.
- `lock.snapshot-unavailable`: Restore the exact Git snapshot, or run a command that writes the lock, then check again.
- No startup migration prompt: The CLI only asks about the user config, and only in an interactive session. Run `agent-marketplace migrate --dry-run` to preview a conversion of any layer.
- A local config still contains `harnesses`: The `--to v2` remediation removes it, because the target harness set belongs in the user or repository config.

See the [configuration reference](../reference/configuration.md) for every field and the full precedence rules.
