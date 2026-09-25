# Command-line reference

The `agent-marketplace` command reads layered configuration, including local files named by each layer's `include` field, discovers registry resources, and materializes those resources into harness-native layouts.

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

Resource installation defaults to `repo`, and endpoint registration defaults to `user`. When you remove a named resource without `--scope`, the command chooses a scope from the selection it finds. `remove --from` defaults to `repo`; `deinit` defaults to `user`.

## `init`

Creates a minimal v4 user config with an explicit harness list. Initialization adds no sources or resources and leaves an existing user config unchanged. Named Git and directory resource selections can subsequently be edited with the v4-aware CLI or TUI paths; other legacy-only editors still refuse unsupported v4 edits.

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

## `search`

Searches hosted Marketplace endpoints without editing configuration. An empty query lists the endpoint's default result page.

```text
agent-marketplace search [query] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--marketplace` | alias, URL, or `all` | no | all configured | Searches one configured alias, one configured or ad hoc HTTP(S) endpoint, or every configured endpoint. |
| `--kind` | string | no | all kinds | Filters by `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. |
| `--output` | string | no | `table` | `table` or `json`. JSON includes successful results and per-endpoint errors. |
| `--limit` | non-negative integer | no | server default `100` | Maximum results after federating endpoint responses. |
| `--offset` | non-negative integer | no | `0` | Result offset after federating endpoint responses. |

Table output has `MARKETPLACE`, `SOURCE`, `KIND`, `NAME`, and `DESCRIPTION` columns. Relevance scores determine ordering but are not printed. Searches across multiple endpoints succeed when at least one endpoint returns results; failed endpoints produce warnings and appear under `errors` in JSON. An explicitly selected single endpoint failure returns an error.

## `install`

Adds selections to one writable config layer and syncs harness output. A named resource from a configured Git or directory source, or from `--from <registry>` with an optional `--ref` and `--path`, can be selected in a v4 destination; an ad hoc registry becomes a named v4 `git` or `directory` source. Installing every resource from an ad hoc registry into a v4 destination is refused; name the resource or declare an `all: true` resource group. Other legacy-only editing paths, including some bulk, tag, and hosted Marketplace operations, may still refuse v4 destinations with `v4.writer-opt-in` or a source-specific diagnostic; a refusal leaves the authored configuration unchanged. See [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

```text
agent-marketplace install <resource-name> [flags]
agent-marketplace install <marketplace-endpoint> [--scope user|repo|local]
agent-marketplace install <marketplace-locator> [flags]
agent-marketplace install --marketplace <alias-or-url> <resource-locator> [flags]
agent-marketplace install --tag <tag> [flags]
agent-marketplace install --all [flags]
agent-marketplace install --from <registry> [<resource-name>] [flags]
```

Marketplace locators have one of these forms:

```text
alias:registry/kind/name[@sha256:<64 lowercase hex>]
https://host/r/registry/kind/name[/revisions/sha256:<64 lowercase hex>]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | endpoint: `user`; resource: `repo` | Configuration layer: `repo`, `user`, or `local`. |
| `--kind` | string | no | `skill` where a kind is needed | Selects `skill`, `command`, `subagent`, `hook`, or `mcp`; also disambiguates a named registry resource. |
| `--tag` | string | no | none | Installs registry resources whose metadata contains the tag. Mutually exclusive with a resource name, `--all`, `--from`, and Marketplace locators. |
| `--all` | boolean | no | `false` | Installs every discovered registry resource of the selected kind. Valid only in `repo` scope. |
| `--registry` | string | no | all configured registries | Limits `--all` to one configured Git registry URL. |
| `--from` | string | no | configured registries | Installs directly from an ad hoc Git URL, GitHub web URL, or GitLab web URL. Without a resource name, installs the registry bundle. |
| `--ref` | string | no | inferred or registry default | Selects a branch, tag, or commit for `--from`. Requires `--from`. |
| `--path` | string | no | inferred or registry root | Selects a subdirectory for `--from`. Requires `--from`. |
| `--marketplace` | alias or endpoint URL | no | locator prefix | Resolves a Marketplace resource locator against the selected endpoint. Valid only for one Marketplace resource. |
| `--as` | safe local name | no | resource name | Changes the generated local name while retaining the Marketplace coordinate. |
| `--accept-risk` | boolean | no | `false` | Accepts the verified revision's warning findings and active capabilities. |
| `--yes` | boolean | no | `false` | Automation spelling of `--accept-risk`. |
| `--verbose` | boolean | no | `false` | Shows the complete collection inventory and technical Marketplace preview, including revisions, digests, acquisition details, and provenance. |

A Marketplace endpoint lookup verifies its well-known identity and canonical URL. To use the endpoint from a v4 root, declare a named `marketplace` source and add an exact request to its `resources` group; keep the matching verified layer lock next to the configuration.

A Marketplace install resolves and verifies the exact artifact before changing config. The default output shows a summary of its source, findings, capabilities, and up to ten collection members. Add `--verbose` to see all collection members, the digest, license, provenance, and acquisition mode. You cannot install rejected or revoked resources. In non-interactive use, pass `--accept-risk` or `--yes` to accept warning findings and active capabilities for that verified invocation only.

In an interactive terminal, warning findings and active capabilities produce an `Install despite these risks? [y/N]:` prompt. Accepting commits the exact prepared revision and prints only the result. Non-interactive execution still requires `--accept-risk` or `--yes` and returns a nonzero exit code without consent.

Successful Marketplace installs print a concise confirmation:

```text
✓ Installed frontend-design
✓ Installed coderabbit-skills · 3 skills
```

Configured Git registry installs retain their scope-specific confirmation, such as `Installed frontend-design in this repo`. Unmet resource prerequisites are reported as warnings and do not install prerequisite resources automatically.

For configured Git registries, discovery stays on the exact inventory of every layer that already has a valid lock. A registry may bootstrap from its mutable ref only when its own configuration layer has no lock; a missing lock in one layer does not make another layer's post-lock content installable.

## `remove`

Removes an explicit selection or records an exclusion, then synchronizes materialized output. For hosted Marketplace pins, removing a direct resource clears its direct selection while preserving any collection ownership; removing a collection clears that collection selector and prunes leaves no longer retained. A hosted resource installed with `--as` can be removed by either its local name or canonical Marketplace name; a name that identifies more than one direct pin is refused. Required organization resources cannot be removed or excluded.

Named removals from configured v4 Git or directory sources use the v4-aware editor and sync the resulting selection. Other legacy-only removal paths can still refuse a v4 destination. Hosted sources have further restrictions; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

```text
agent-marketplace remove <resource-name> [flags]
agent-marketplace remove --from <registry> [<resource-name>] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | inferred for a name; `repo` with `--from` | Configuration layer: `repo`, `user`, or `local`. Specifying a scope leaves selections in other scopes unchanged. |
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

The generated paths are `/etc/agent-marketplace/lock.yaml`, `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`. Commit the repository lock and keep the personal-local lock ignored. An absent organization lock leaves that layer intentionally unlocked: online consumer commands may use mutable Git acquisition or resolve explicit Marketplace requests through a verified in-memory lock, but do not create the organization lock. Offline Marketplace resolution requires a deployed lock. A present organization lock is exact and fail-closed; malformed, untrusted, or mismatched state is not downgraded to transient acquisition. Plain directory registries are live and are not represented in these locks.

## `sync`

Resolves selected resources from exact Git snapshots and reconciles native harness output with configuration. Organization requirements use mutable acquisition when that externally managed layer intentionally has no lock.

```text
agent-marketplace sync [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--check` | boolean | no | `false` | Computes lock and output drift without reconciling either. Returns exit status 1 when state is out of sync. |
| `--locked` | boolean | no | `false` | Requires fresh lockfiles and forbids config or lock writes. |
| `--offline` | boolean | no | `false` | Uses only trusted local Git and Marketplace caches and never contacts remote endpoints; implies locked dependency behavior. Cached revocation data may be stale. |
| `--allow-revoked` | exact Marketplace revision target; repeatable | no | none | Allows one revoked pin for this execution: `marketplace/registry/kind/name@sha256:<64 lowercase hex>`. Cannot be combined with `--check`. |
| `--no-backup` | boolean | no | `false` | Skips the configured pre-sync backup of machine-scope harness directories. Mandatory shared-aggregate safety backups are unaffected. |
| `--strict` | boolean | no | `false` | Promotes selected-but-missing and selected-but-malformed warnings to errors. |
| `--prune-stale` | boolean | no | `false` | Removes direct, non-required stale selections from configuration after catalogs load successfully, then synchronizes again. Cannot be combined with `--locked` or `--offline`. |
| `-v`, `--verbose` | boolean | no | `false` | Prints affected files and malformed-resource details instead of summary-only output. |

Normal synchronization prints counts for installed, removed, malformed, and failed resources:

```text
Installed 3  Removed 1  Malformed 0  Errors 0
```

With `--check`, an in-sync result produces no verdict text and returns exit status 0. Drift prints a summary such as `sync: 2 out of sync (1 missing, 1 orphaned)`, lists planned changes, and returns exit status 1. `--check` is read-only across config, locks, caches, transaction state, and materialized output: it neither recovers pending transactions nor fetches a cold exact snapshot, and reports `lock.snapshot-unavailable` when required snapshot bytes are absent. `--prune-stale` cannot be combined with `--locked` or `--offline` because pruning changes configuration.

Pending dependency transactions cause `sync --locked`, `sync --offline`, `sync --check`, `lock --check`, `list`, and `status` to fail with `dependency.recovery-required` before loading configuration or registry state. These commands leave config, locks, journal evidence, and transaction-state permissions unchanged. Run a writing `sync` or `lock` without those flags to recover, then retry the inspection. An absent transaction-state directory does not require recovery and is not created by inspection.

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

Output reports the number of refreshed Git registries and, with `--bump`, the number of rewritten refs. Marketplace mirror pins are independently checked for newer usable revisions; index-only pins and organization-required pins are reported as skipped when the consumer cannot rewrite them.

```text
Updated 2 registries, bumped 1 refs
Marketplace pins: updated 1, skipped 1
Marketplace https://marketplace.example.com source/skill/review: updated sha256:... -> sha256:...
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
| `MARKETPLACE PINS` | Endpoint identity, registry, resource coordinate, installed name, exact revision and digest, acquisition mode, source layer, revocation/current state, cache state, and update availability. |
| `HARNESS TARGETS` | Target capabilities and resolved repository and machine roots. |
| `SYNCED RESOURCES` | Resource kind, name, harness, target path, registry, ref, cache state, source, and collection origin. |
| `LOCALLY EXCLUDED` | Resources removed by repository-personal exclusions. |
| `LOCAL REGISTRIES` | Registry entries declared in repository-personal configuration. |
| `ORPHANED SYMLINKS` | Generated links that a sync would remove. |
| `NAME COLLISIONS` | Registry overlap and resource-name collision diagnostics. |
| `UNMET REQUIREMENTS` | Advisory resource prerequisites and install hints. |
| `ERRORS` | Configuration, catalog, or drift-inspection failures. |

Fatal resolution errors return exit status 1 after the report is printed.

## Configuration authoring

The CLI reads v4 roots and adapts their `sources` and `resources` model for discovery, status, synchronization, and materialization. A root must declare `apiVersion: agent-marketplace/v4`; included fragments may omit `apiVersion` and inherit v4 from the root.

Source kinds are explicit:

- `git` sources require `ref` and use the owning layer's exact Git lock.
- `directory` sources are live and never acquire a Git lock. Relative paths in repository `.agents/config.yaml` and `.agents/config.local.yaml` roots use the repository root; user/organization configs and included fragments use the declaring directory.
- `marketplace` sources require a matching verified layer lock for explicit requests.

Use the [v4 configuration reference](configuration.md) to write source declarations and resource groups directly. Named Git and directory selections also have v4-aware CLI and TUI editors; unsupported legacy-only editors fail closed. The CLI rejects unknown fields, mixed source kinds, invalid locators, and Marketplace lock evidence that does not match. Writable-layer locks can be generated online for explicit hosted requests; an intentionally absent organization lock permits verified transient online acquisition without an organization write. Offline transient acquisition, hosted update/rebind, dynamic-selector snapshots, and general hosted editing remain unsupported.

## `migrate`

Converts representable legacy user, repository, and repository-personal configuration to the v4 source/resource schema. Migration is transactional, preserves v2/v3 reads when declined or refused, and uses verified local Marketplace cache evidence without network access. Use `--to v2` for legacy structural remediation.

```text
agent-marketplace migrate [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--to` | string | no | `v4` | Target schema: `v4` or `v2`. |
| `--scope` | string | no | `all` | V4 writable layer: `all`, `user`, `repo`, or `local`. |
| `--dry-run` | boolean | no | `false` | Previews v4 migration without writing config or lock files. |
| `--harnesses` | comma-separated strings | v2 only | none | Target harnesses for legacy structural remediation. |

The `--to v4` spelling is accepted as an alias. V4 migration refuses lossy inputs and incomplete Marketplace provenance without changing files; proven Marketplace conversions create or upgrade the owning v2 lock atomically with the migrated config. Organization configuration is never edited.

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

A partial conversion writes complete resources, reports skipped resources and diagnostics, suppresses unsafe pruning when applicable, and returns exit status 1. Source and output paths are canonicalized, including existing symlinks. Conversion refuses equal, nested, or ancestor relationships before reading or mutating either tree, including with `--sync`.

Codex conversion refuses a destination that already contains shared `.codex/hooks.json` or `.codex/config.toml` configuration. Devin conversion also refuses an existing shared destination (`mcp_config.json`, `hooks.v1.json`, or the user `config.json` hook aggregate) rather than adopting or merging it. Both targets stage all artifacts, validate staged aggregate syntax, publish with guarded hard links, and roll back only artifacts published by that transaction when publication fails. One-shot conversion does not create durable aggregate-ownership state and never adopts a destination; sync also does not adopt pre-existing entries. Only explicit `doctor --repair` may adopt an entry, and only when its bytes exactly match the desired generated value. Missing or corrupt ownership state remains fail-closed.

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

Removes mutable and exact Git cache entries and unreferenced hosted Marketplace artifacts that are no longer reachable from validated configuration and active locks in the **current context**: organization, user, current repository, and personal-local layers, including their applicable includes. Exact Git snapshots are retained by locked source and commit; Git object stores are retained by canonical source URL from either a lock or configuration, including organization sources with no organization lock. Marketplace blobs, manifests, revision metadata, and index-only occurrence snapshots are retained by exact configured pins. Unreferenced object stores and abandoned staging directories are collected. Protected derived-cache namespaces and persistent coordination locks are retained.

The cache is shared across repositories, but `gc` does not inventory other repositories. Running it in repository A can evict a source used only by repository B. Later use in B may require reacquisition from the remote and can fail offline or if the remote no longer serves the locked commit. Use `--dry-run` to inspect current-context eviction before deleting.

Pending dependency-transaction recovery runs first and holds the shared exact-cache lease while publishing recovered config and lock state. Collection then takes the exclusive exact-cache lease before mutable-cache exclusion and reloads all applicable configuration layers and their locks before deriving reachability. Missing required locks, malformed configuration or locks, and untrusted locks block destructive collection; a present organization lock must also match its configuration.

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

## `serve`

Runs a hosted Marketplace from a separate [Marketplace operator configuration](marketplace-operator-configuration.md).

```text
agent-marketplace serve --config <path> [--role api|worker|all]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--config` | path | yes | none | Marketplace operator YAML file. |
| `--role` | string | no | `all` | `api` runs the public and operations listeners, `worker` runs background indexing, and `all` runs both. |

Startup opens the configured stores, ensures the Marketplace instance exists, and applies configured revocations before either role becomes ready. The command runs until its context is cancelled or a role fails.

### `serve reindex`

Enqueues and processes refresh work outside the scheduler.

```text
agent-marketplace serve reindex [registry-slug-or-id] --config <path>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--config` | path | yes | none | Marketplace operator YAML file. |

With no registry argument, the command reindexes every configured registry. With one slug or ID, it reindexes only that registry. It applies revocations before acquisition and performs a store readiness check after processing.

### `serve adopt`

Adopts catalog settings during a controlled maintenance window, establishing database configuration authority.

```text
agent-marketplace serve adopt --config <path> --actor <id> --reason <text> --idempotency-key <key> --confirm-maintenance
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--actor` | string | yes | none | Operator identity recorded in the maintenance audit. |
| `--config` | path | yes | none | Path to the Marketplace operator config file. |
| `--confirm-maintenance` | boolean | no | `false` | Confirms all API and worker processes are stopped, backups exist, and shared caches are invalidated. |
| `--idempotency-key` | string | yes | none | Stable key reused when retrying this adoption. |
| `--reason` | string | yes | none | Reason recorded in the adoption audit. |

All API and worker processes must be stopped, and database and blob storage backups must be created before running adoption. Requires `management.mode: database` in the configuration file.

## `deinit`

Removes marketplace-managed artifacts for one scope. User scope also removes orphaned registry caches while preserving currently resolvable organization-owned machine artifacts. A present organization lock is inspected exactly and any invalid lock blocks teardown. When the organization lock is absent, normal deinit may refresh the mutable registry to preserve its output and cache; `--dry-run` is inspection-only, so an uncached unlocked organization registry produces a warning rather than a fetch. Configuration files remain unless `--purge-config` is set.

```text
agent-marketplace deinit [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `user` | Teardown scope: `user`, `repo`, or `local`. `repo` and `local` both target repository-scoped artifacts. |
| `--purge-config` | boolean | no | `false` | Also removes the configuration file and its owning generated YAML lock for the selected scope, including an orphaned lock. Other layers and included fragments are preserved. Stable transaction coordination files and unrelated recovery journals are retained; an overlapping pending transaction blocks purge until recovered. |
| `--dry-run` | boolean | no | `false` | Reports artifacts, caches, configuration files, and generated YAML locks that would be removed without deleting them. |

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

Launches the interactive terminal UI for browsing Git registries and hosted Marketplaces, editing selections, selecting harnesses, and synchronizing.

```text
agent-marketplace tui
```

The command resolves the repository root from the current directory and uses the current user's home directory for machine-scoped configuration and output. It renders configured resources before network discovery, loads up to 50 results from each Marketplace, and starts federated Marketplace search 250 milliseconds after search input settles. Discovery cache entries are fresh for five minutes and may be used as stale fallback for up to 24 hours.

Marketplace installation verifies the exact revision and displays its findings, capabilities, provenance, and digest before confirmation. Blocked and revoked resources remain visible but cannot be installed.

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
