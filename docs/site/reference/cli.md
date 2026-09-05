# Command-line reference

The `agent-marketplace` command reads layered configuration, including local files named by each layer's `include` field, discovers registry resources, and materializes those resources into harness-native layouts. This page defines the command hierarchy, arguments, flags, output contracts, and exit statuses.

For configuration fields, include syntax, and precedence, see the [repository configuration reference](configuration.md).

## Command syntax

```text
agent-marketplace [global flags]
agent-marketplace <command> [arguments] [flags]
```

A bare invocation starts the terminal UI when standard input is a terminal. In a non-terminal context it prints help. Setting `AGENT_MARKETPLACE_NO_TUI` to a non-empty value also makes a bare invocation print help.

## Global flags

Global flags are accepted by every command.

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--debug` | boolean | no | `false` | Enables debug logging and mirrors logs to standard error. |
| `--diagnostic` | boolean | no | `false` | Captures a diagnostic telemetry payload for review for the current invocation. |
| `--diagnostic-accept-sensitive-data` | boolean | no | `false` | Acknowledges that a diagnostic payload may contain sensitive data. |
| `-h`, `--help` | boolean | no | `false` | Prints help for the selected command. |
| `--version` | boolean | no | `false` | Prints the CLI version. This flag belongs to the root command. |

## Resource kinds

Flags named `--kind` accept the following values unless a command states otherwise:

| Value | Resource |
|---|---|
| `skill` | Agent skill |
| `command` | Harness command |
| `subagent` | Subagent definition |
| `hook` | Harness hook |
| `mcp` | MCP server definition |
| `collection` | Named resource collection; accepted only by `list` |

When `install` or `remove` receives a name shared by multiple resource kinds, `--kind` selects the intended kind.

## Configuration scopes

`install`, `remove`, and `deinit` use these scope names:

| Scope | Configuration file | Materialization scope |
|---|---|---|
| `repo` | `<repo>/.agents/config.yaml` | Repository |
| `user` | `~/.agents/config.yaml` | Machine |
| `local` | `<repo>/.agents/config.local.yaml` | Repository, personal to the checkout |

`repo` is the default for `install` and `remove`. `user` is the default for `deinit`.

## `init`

Creates a minimal user configuration with the canonical v2 schema and an explicit harness list. It does not add registries or install resources. An existing user configuration is left unchanged.

```text
agent-marketplace init [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--harnesses` | comma-separated strings | non-terminal only | none | Target harnesses. Accepted values: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, and `windsurf`. |

Without `--harnesses`, terminal execution detects installed harnesses and prompts for confirmation or editing. Non-terminal execution requires `--harnesses`. Interactive creation also requests telemetry consent unless organization policy already fixes the telemetry level.

Output for a newly created file has this form:

```text
Initialized user personal config: <home>/.agents/config.yaml
```

## `list`

Lists discovered resources from configured registries or from one ad hoc registry. The command does not edit configuration. A configured target harness set is optional; its absence produces a warning rather than an error.

```text
agent-marketplace list [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--kind` | string | no | all kinds | Filters by `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. |
| `--from` | string | no | configured registries | Uses an ad hoc Git URL, GitHub web URL, or GitLab web URL without writing it to configuration. |
| `--ref` | string | no | inferred or registry default | Selects a branch, tag, or commit for `--from`. Requires `--from`. |
| `--path` | string | no | inferred or registry root | Selects a subdirectory for `--from`. Requires `--from`. |

The table columns are `REGISTRY`, `KIND`, `NAME`, `TAGS (metadata)`, and `STATUS`. Configured-registry statuses include `available`, `enabled`, `required (org)`, and malformed-resource details. Ad hoc resources have status `available`.

When configured output is out of sync, the command appends a summary with this form:

```text
3 resources out of sync — run 'agent-marketplace sync'
```

## `install`

Adds selections to one configuration layer and synchronizes materialized output. It supports one named resource, one collection, tag selection, all resources of one kind, or an ad hoc registry.

```text
agent-marketplace install <resource-name> [flags]
agent-marketplace install --tag <tag> [flags]
agent-marketplace install --all [flags]
agent-marketplace install --from <registry> [<resource-name>] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `repo` | Configuration layer: `repo`, `user`, or `local`. |
| `--kind` | string | no | `skill` where a kind is needed | Selects `skill`, `command`, `subagent`, `hook`, or `mcp`; also disambiguates a named resource. |
| `--tag` | string | no | none | Installs resources whose metadata contains the tag. Mutually exclusive with a resource name, `--all`, and `--from`. |
| `--all` | boolean | no | `false` | Installs every discovered resource of the selected kind. Valid only in `repo` scope and mutually exclusive with a resource name, `--tag`, and `--from`. |
| `--registry` | string | no | all configured registries | Limits `--all` to one configured registry URL. Valid only with `--all`. |
| `--from` | string | no | configured registries | Installs directly from an ad hoc Git URL, GitHub web URL, or GitLab web URL. Without a resource name, installs the registry bundle. |
| `--ref` | string | no | inferred or registry default | Selects a branch, tag, or commit for `--from`. Requires `--from`. |
| `--path` | string | no | inferred or registry root | Selects a subdirectory for `--from`. Requires `--from`. |

A successful named installation prints a scope-specific confirmation:

```text
Installed frontend-design in this repo
Installed frontend-design user-wide
Installed frontend-design locally
```

Collection installation prints `Installed collection <name> <scope-label>`. Unmet resource prerequisites are reported as warnings and do not install prerequisite resources automatically.

For configured registries, discovery stays on the exact inventory of every layer that already has a valid lock. A registry may bootstrap from its mutable ref only when its own configuration layer has no lock; a missing lock in one layer does not make another layer's post-lock content installable.

## `remove`

Removes an explicit selection or records an exclusion, then synchronizes materialized output. Required organization resources cannot be removed or excluded.

```text
agent-marketplace remove <resource-name> [flags]
agent-marketplace remove --from <registry> [<resource-name>] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `repo` | Configuration layer: `repo`, `user`, or `local`. |
| `--kind` | string | no | inferred | Selects `skill`, `command`, `subagent`, `hook`, or `mcp` when a name is ambiguous. |
| `--from` | string | no | configured registries | Targets an ad hoc registry entry. Without a resource name, removes or excludes the entire entry for the selected scope. |
| `--ref` | string | no | none | Refines the registry entry selected by `--from`. Requires `--from`. |
| `--path` | string | no | none | Refines the registry entry selected by `--from`. Requires `--from`. |

Success output distinguishes removed selections, exclusions, collections, and registry entries:

```text
Removed frontend-design
Excluded frontend-design (provided by web-development)
Removed registry entry https://github.com/example/resources.git
Excluded all resources from https://github.com/example/resources.git (local scope)
```

## `lock`

Resolves remote Git requirements to exact commits and verified source manifests in generated, layer-owned locks. With the default `all` scope it writes user, repository, and personal-local locks; organization locks are written only when selected explicitly.

```text
agent-marketplace lock [resource ...] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `all` | Lock scope: `user`, `repo`, `local`, `all`, or `organization`. |
| `--check` | boolean | no | `false` | Reports whether selected locks would change without writing. A required change returns exit status 1. |
| `--upgrade` | boolean | no | `false` | Refreshes all or targeted Git resolutions to their current refs. |
| `--registry` | string | conditionally | none | Targets a configured Git registry during `--upgrade`. |
| `--ref` | string | no | none | Narrows `--registry` to a declared ref. |
| `--path` | string | no | none | Narrows `--registry` to a locked source root. |
| `--kind` | string | conditionally | none | Resource kind for named `--registry` upgrade targets. |

Resource names and the registry targeting flags require `--upgrade`; names also require `--registry` and `--kind`. A resource-level upgrade expands to its containing Git source because the commit is the atomic acquisition unit.

`lock --check` inspects existing config, lock, and exact-snapshot bytes without transaction recovery, remote resolution, or cache population. A fresh lock whose exact snapshot is absent from a cold cache reports `lock.snapshot-unavailable`; a stale or missing lock reports the required change without fetching it. A writing reconciliation holds the shared exact-cache lease from before its first snapshot acquisition or verification through config-and-lock publication, preventing garbage collection from deleting a new snapshot before the published lock makes it reachable.

The generated paths are `/etc/agent-marketplace/lock.yaml`, `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`. Commit the repository lock and keep the personal-local lock ignored. An absent organization lock leaves that layer intentionally unlocked: consumer commands may use mutable acquisition or cache inspection but do not create the organization lock. A present organization lock is exact and fail-closed; malformed or untrusted lock state is not downgraded to mutable discovery. Plain directory registries are live and are not represented in these Git locks. Hosted Marketplace locking is not part of this foundation.

## `sync`

Resolves selected resources from exact Git snapshots and reconciles native harness output with configuration. Organization requirements use mutable acquisition when that externally managed layer intentionally has no lock.

```text
agent-marketplace sync [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--check` | boolean | no | `false` | Computes lock and output drift without reconciling either. Returns exit status 1 when state is out of sync. |
| `--locked` | boolean | no | `false` | Requires fresh lockfiles and forbids config or lock writes. |
| `--offline` | boolean | no | `false` | Uses only trusted local Git caches and never fetches remote endpoints; implies locked dependency behavior. |
| `--no-backup` | boolean | no | `false` | Skips the configured pre-sync backup of machine-scope harness directories. Mandatory shared-aggregate safety backups are unaffected. |
| `--strict` | boolean | no | `false` | Promotes selected-but-missing and selected-but-malformed warnings to errors. |
| `--prune-stale` | boolean | no | `false` | Removes direct, non-required stale selections from configuration after catalogs load successfully, then synchronizes again. |
| `-v`, `--verbose` | boolean | no | `false` | Prints affected files and malformed-resource details instead of summary-only output. |

Normal synchronization prints counts for installed, removed, malformed, and failed resources:

```text
Installed 3  Removed 1  Malformed 0  Errors 0
```

With `--check`, an in-sync result produces no verdict text and returns exit status 0. Drift prints a summary such as `sync: 2 out of sync (1 missing, 1 orphaned)`, lists planned changes, and returns exit status 1. `--check` is read-only across config, locks, caches, transaction state, and materialized output: it neither recovers pending transactions nor fetches a cold exact snapshot, and reports `lock.snapshot-unavailable` when required snapshot bytes are absent. `--prune-stale` cannot be combined with `--locked` or `--offline` because pruning changes configuration.

## `update`

Advances selected Git locks and synchronizes enabled resources. Without targeting flags it updates all writable scopes.

```text
agent-marketplace update [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--bump` | boolean | no | `false` | Rewrites eligible registry refs to newer matching tags before advancing locks. |
| `--scope` | string | no | `all` | Update scope: `user`, `repo`, `local`, or `all`. |
| `--registry` | string | no | none | Targets a configured Git registry. |
| `--ref` | string | no | none | Narrows `--registry` to a declared ref. |
| `--path` | string | no | none | Narrows `--registry` to a locked source root. |
| `--kind` | string | conditionally | none | Resource kind for named registry targets. |

Output reports the number of refreshed registries and, with `--bump`, the number of rewritten refs:

```text
Updated 2 registries, bumped 1 refs
```

## `status`

Reports effective configuration, selected resources, target harnesses, roots, registry refs, cache freshness, source layers, drift, exclusions, collisions, unmet requirements, and errors. It inspects state without reconciling output.

```text
agent-marketplace status
```

The report can contain these sections:

| Section | Contents |
|---|---|
| `TARGET HARNESS SET` | Effective harness list and the configuration layer that supplied it. |
| `EXACT REGISTRY LOCKS` | Source layer, canonical Git origin, declared ref, exact commit, and immutable snapshot path. |
| `HARNESS TARGETS` | Target capabilities and resolved repository and machine roots. |
| `SYNCED RESOURCES` | Resource kind, name, harness, target path, registry, ref, cache state, source, and collection origin. |
| `LOCALLY EXCLUDED` | Resources removed by repository-personal exclusions. |
| `LOCAL REGISTRIES` | Registry entries declared in repository-personal configuration. |
| `ORPHANED SYMLINKS` | Generated links that a sync would remove. |
| `NAME COLLISIONS` | Registry overlap and resource-name collision diagnostics. |
| `UNMET REQUIREMENTS` | Advisory resource prerequisites and install hints. |
| `ERRORS` | Configuration, catalog, or drift-inspection failures. |

Fatal resolution errors return exit status 1 after the report is printed.

## `migrate`

Performs transactional structural remediation across user, repository, and repository-personal configuration files. Valid v2 and v3 `apiVersion` values are preserved. Files containing legacy `identity` or `team_profiles` fields are refused without modification.

```text
agent-marketplace migrate [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--harnesses` | comma-separated strings | conditionally | existing user harnesses | Supplies the user harness set when remediation requires one. Accepted values are the seven supported harness targets. |

When a harness set is required but absent, terminal execution prompts and non-terminal execution requires `--harnesses`. All inputs are validated before any file is written.

Output identifies each changed file, or reports:

```text
No structural remediation needed; config files are already valid
```

## `convert`

Ingests one harness layout and writes an equivalent layout for another harness. Conversion does not edit marketplace configuration.

```text
agent-marketplace convert --from <harness> --to <harness> <path> [flags]
agent-marketplace convert --autodetect --to <harness> <path> [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--from` | string | unless `--autodetect` | none | Source harness: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, or `windsurf`. |
| `--autodetect` | boolean | no | `false` | Detects the source harness from layout markers instead of requiring `--from`. |
| `--to` | string | yes | none | Target harness. Accepts the same seven values as `--from`. |
| `--out` | path | no | `<path>-<to>` | Output directory. |
| `--sync` | boolean | no | `false` | Uses sync write mode and prunes stale output files. Without it, conversion uses overwrite mode. |
| `--dry-run` | boolean | no | `false` | Reports planned writes without changing output files. |

Output includes the resolved source, target, output directory, and operation counts:

```text
Converted claude -> factory into plugin-factory
Written 4, Skipped 0, Removed 0, Diagnostics 0
```

A partial conversion writes complete resources, reports skipped resources and diagnostics, suppresses unsafe pruning when applicable, and returns exit status 1. Codex conversion refuses a destination that already contains shared `.codex/hooks.json` or `.codex/config.toml` configuration.

## `doctor`

Reports repository detection, configuration diagnostics, harness capabilities and roots, Devin CLI compatibility, and unmet resource requirements.

```text
agent-marketplace doctor [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--repair` | boolean | no | `false` | Previews recovery actions for Devin and Codex shared-aggregate ownership state and, on a terminal, requests confirmation for applicable actions. |
| `--yes` | boolean | no | `false` | With `--repair`, adopts byte-identical desired entries without prompting. It never automatically removes or replaces entries. |

`--repair` classifies exact adoption matches, evidence-backed orphan candidates, and unmanaged value conflicts. Non-terminal execution is preview-only unless `--yes` can perform safe, byte-neutral adoption. Orphan removal and conflict replacement always require per-entry terminal confirmation and a fresh backup. `--yes` without `--repair` is an error.

Compatibility findings do not block synchronization. Repair failures, corrupt ownership state, and unparseable aggregate files return exit status 1.

## `backup`

Manages backups of machine-scope harness output.

```text
agent-marketplace backup <command>
```

### `backup create`

Creates an on-demand backup for currently managed machine paths and applies configured backup rotation.

```text
agent-marketplace backup create
```

When backups are disabled, the command exits successfully after printing `Backup is disabled in config; skipping.` When no machine paths are managed, it prints `No managed paths to back up.`

### `backup list`

Prints the backup directory followed by backup timestamps grouped by harness target.

```text
agent-marketplace backup list
```

### `backup restore`

Overlays the latest backup onto the selected machine harness root. Existing unrelated files are preserved.

```text
agent-marketplace backup restore --target <harness>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `-t`, `--target` | string | yes | none | Harness target: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, or `windsurf`. |

The command refuses a non-directory target root and a target root that is a symbolic link.

## `gc`

Removes mutable and exact Git cache entries that are no longer referenced by validated configuration and active locks. Pending dependency-transaction recovery runs first and holds the shared exact-cache lease while publishing recovered config and lock state. Collection then takes the exclusive lease and reloads the complete effective configuration and every layer lock before deriving reachability; a missing, stale, malformed, or untrusted required lock blocks destructive collection. Protected derived-cache namespaces are retained.

```text
agent-marketplace gc [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--dry-run` | boolean | no | `false` | Lists cache entries that would be removed without deleting them. |

The final line reports the number of affected entries:

```text
Removed 2 cache entries
Would remove 2 cache entries
```

## `deinit`

Removes marketplace-managed artifacts for one scope. User scope also removes orphaned registry caches while preserving currently resolvable organization-owned machine artifacts. A present organization lock is inspected exactly and any invalid lock blocks teardown. When the organization lock is absent, normal deinit may refresh the mutable registry to preserve its output and cache; `--dry-run` is inspection-only, so an uncached unlocked organization registry produces a warning rather than a fetch. Configuration files remain unless `--purge-config` is set.

```text
agent-marketplace deinit [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `user` | Teardown scope: `user`, `repo`, or `local`. `repo` and `local` both target repository-scoped artifacts. |
| `--purge-config` | boolean | no | `false` | Also removes the configuration file for the selected scope. |
| `--dry-run` | boolean | no | `false` | Reports artifacts, caches, and configuration files that would be removed without deleting them. |

Unreadable configuration does not block teardown. In that case the command reports a warning, applies default materialization-mode assumptions, and notes that artifacts created in another mode may remain.

## `telemetry`

Reads and modifies user telemetry preferences and local telemetry state. Organization policy can impose a minimum level or lock an endpoint.

```text
agent-marketplace telemetry <command>
```

### `telemetry status`

Prints the effective level and source, endpoint hostname and source, header references, retention policy, community-reporting state, management status, installation ID, queue statistics, retained diagnostic count, last delivery failure, and organization-locked fields.

```text
agent-marketplace telemetry status
```

Header values are not printed.

### `telemetry enable`

Sets the user telemetry level and optionally a user-managed OTLP/HTTP endpoint. The user configuration must already exist.

```text
agent-marketplace telemetry enable --level <level> [--endpoint <url>]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--level` | string | yes | none | `basic` or `detailed`. `off` is rejected; `telemetry disable` represents that state. |
| `--endpoint` | URL string | no | existing effective endpoint | Stores a user endpoint unless organization policy locks it. |

A requested level below an organization-required level is rejected.

### `telemetry disable`

Sets the user telemetry preference to `off`.

```text
agent-marketplace telemetry disable
```

Organization policy can keep the effective level active; the command reports that state after storing the user preference.

### `telemetry preview`

Writes a representative payload locally without sending it.

```text
agent-marketplace telemetry preview --level <level>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--level` | string | yes | none | Preview level: `basic`, `detailed`, or `diagnostic`. |

A diagnostic preview requires terminal confirmation. In non-terminal execution it requires the global `--diagnostic-accept-sensitive-data` flag. Output identifies the local payload path and resolved destination metadata.

### `telemetry purge`

Removes queued telemetry records and retained diagnostic payloads. It does not reset the installation ID or change telemetry preferences.

```text
agent-marketplace telemetry purge
```

### `telemetry reset-id`

Replaces the installation ID and purges pending telemetry associated with the old ID.

```text
agent-marketplace telemetry reset-id
```

Output includes the newly generated installation ID.

## `tui`

Launches the interactive terminal UI for browsing catalogs, editing selections, selecting harnesses, and synchronizing.

```text
agent-marketplace tui
```

The command resolves the repository root from the current directory and uses the current user's home directory for machine-scoped configuration and output.

## `completion`

Generates shell completion scripts through Cobra's built-in completion commands.

```text
agent-marketplace completion bash [--no-descriptions]
agent-marketplace completion zsh [--no-descriptions]
agent-marketplace completion fish [--no-descriptions]
agent-marketplace completion powershell [--no-descriptions]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--no-descriptions` | boolean | no | `false` | Omits command and flag descriptions from generated completions. |

The generated script is written to standard output.

## Exit statuses

| Status | Condition |
|---:|---|
| `0` | The command completed successfully. Warnings such as an absent target set for `list`, unmet advisory prerequisites, or non-blocking compatibility findings do not change this status. |
| `1` | Argument validation, configuration loading, registry access, resolution, conversion, materialization, repair, backup, or filesystem work returned an error. |
| `1` | `sync --check` detected drift. |
| `1` | `sync --strict` encountered a selected resource that was missing or malformed. |

The CLI does not define additional command-specific numeric statuses. Cobra argument errors and runtime errors are printed to standard error; command reports and normal results are printed to standard output.
