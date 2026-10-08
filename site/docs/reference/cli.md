# Command-line reference

This page lists every `agent-marketplace` command, its flags, defaults, output, and exit status. Use it when you need the exact behavior of a command. If you are new to the terminal, start with [terminal basics](../terminal-basics.md) and the [quick start](../quick-start.md).

The `agent-marketplace` command reads your layered configuration (including local files named by each layer's `include` field), finds the resources in each configured registry, and writes them into the folders each [harness](../glossary.md#harness) reads. The docs call that last step materialization.

For configuration fields, include syntax, and precedence, see the [configuration reference](configuration.md).

## Command syntax

```text
agent-marketplace [global flags]
agent-marketplace <command> [arguments] [flags]
```

Running `agent-marketplace` with no command starts the terminal UI when standard input is a terminal. Otherwise it prints help. Setting `AGENT_MARKETPLACE_NO_TUI` to any non-empty value also makes a bare invocation print help.

Before most commands run, the CLI checks whether the user config (`~/.agents/config.yaml`) still uses a legacy v2 or v3 schema. When standard input is a terminal, it lists the file and asks `Migrate these configuration files to v4 now? [Y/n]:`. Pressing Enter or answering `y` migrates. Answering `n` continues with the legacy reader, and any other answer stops the command with `unknown response`. A refused migration prints a warning and also continues. Without a terminal, the CLI logs a warning and changes nothing. The check does not run for `init`, `migrate`, `convert`, `serve`, `telemetry`, `tui`, `doctor`, or `--version`. The bare terminal UI shows its own migration prompt instead. Repository and personal-local configs are migrated only by an explicit [`migrate`](#migrate).

## Global flags

Every command accepts these flags.

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--debug` | boolean | no | `false` | Enables debug logging and mirrors logs to standard error. |
| `--diagnostic` | boolean | no | `false` | Captures a diagnostic telemetry payload for this invocation so you can review it before it is sent. Capture needs the effective telemetry level `detailed`; at other levels it is turned off. See the [telemetry reference](telemetry.md#diagnostic-payloads). |
| `--diagnostic-accept-sensitive-data` | boolean | no | `false` | Acknowledges that a diagnostic payload may contain sensitive data. Without a terminal, diagnostic export and `telemetry preview --level diagnostic` require it. |
| `-h`, `--help` | boolean | no | `false` | Prints help for the selected command. |
| `--version` | boolean | no | `false` | Prints the CLI version, for example `v4.0.9`. This flag belongs to the root command only. |

## Resource kinds

Flags named `--kind` accept these values unless a command says otherwise:

| Value | Resource |
|---|---|
| `skill` | Agent skill |
| `command` | Harness command |
| `subagent` | Subagent definition |
| `hook` | Harness hook |
| `mcp` | MCP server definition |
| `collection` | Named group of resources. `list`, `search`, and `remove` accept it as a `--kind` value; `install` takes a collection by name. |

When `install` or `remove` gets a name that more than one resource kind uses, `--kind` selects the one you mean.

## Configuration scopes

`install`, `remove`, and `deinit` use these scope names:

| Scope | Configuration file | Where output goes |
|---|---|---|
| `repo` | `<repo>/.agents/config.yaml` | The repository |
| `user` | `~/.agents/config.yaml` | Your machine (home directory) |
| `local` | `<repo>/.agents/config.local.yaml` | The repository, personal to your checkout |

Installing a resource defaults to `repo`, and registering a Marketplace endpoint defaults to `user`. When you remove a named resource without `--scope`, the command picks the scope from the selection it finds. `remove --from` defaults to `repo`, and `deinit` defaults to `user`.

## `init`

Creates a minimal v4 user config (`~/.agents/config.yaml`) with an explicit harness list. It adds no sources or resources. If the file already exists, `init` leaves it unchanged and prints `User personal config already initialized.` You can later edit named Git and directory resource selections with the v4-aware CLI and TUI paths. Other legacy-only editors still refuse edits they cannot make to a v4 file.

```text
agent-marketplace init [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--harnesses` | comma-separated strings | without a terminal | none | Target harnesses. Accepted values: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, and `windsurf`. |

Without `--harnesses`, `init` detects installed harnesses, prints `Detected harnesses: ...`, and asks `Use these? [Y/n/e to edit]:`. Without a terminal, `--harnesses` is required. When `init` creates the file interactively, it also asks `Enable basic telemetry? [Y/n]:`, unless organization policy already sets the telemetry level.

A new file produces this output:

```text
Initialized user personal config: <home>/.agents/config.yaml
```

## `list`

Lists the resources found in your configured registries, or in one ad hoc registry. It does not edit configuration. A target harness set is optional here; without one, `list` prints a warning to standard error and continues.

```text
agent-marketplace list [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--kind` | string | no | all kinds | Filters by `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. |
| `--from` | string | no | configured registries | Reads an ad hoc Git URL, GitHub web URL, or GitLab web URL without writing it to configuration. |
| `--ref` | string | no | inferred or registry default | Selects a branch, tag, or commit for `--from`. Requires `--from`. |
| `--path` | string | no | inferred or registry root | Selects a subdirectory for `--from`. Requires `--from`. |

The table columns are `REGISTRY`, `KIND`, `NAME`, `TAGS (metadata)`, and `STATUS`. Resources from configured registries have status `available`, `enabled`, or `required (org)`. A resource that failed validation has status `malformed: <reason>`. Valid ad hoc resources have status `available`, and ones that fail validation show `malformed: <reason>`.

When configured output is out of sync, the command appends a summary like this:

```text
3 resources out of sync — run 'agent-marketplace sync'
```

## `search`

Searches hosted [Marketplace](../glossary.md#marketplace) endpoints. It does not edit configuration. An empty query returns the endpoint's default result page.

```text
agent-marketplace search [query] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--marketplace` | alias, URL, or `all` | no | all configured | Searches one configured alias, one configured or ad hoc HTTP(S) endpoint, or every configured endpoint. |
| `--kind` | string | no | all kinds | Filters by `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. |
| `--output` | string | no | `table` | `table` or `json`. JSON includes successful results and per-endpoint errors. |
| `--limit` | non-negative integer | no | server default `100` | Maximum results after combining endpoint responses. |
| `--offset` | non-negative integer | no | `0` | Result offset after combining endpoint responses. |

Table output has `MARKETPLACE`, `SOURCE`, `KIND`, `NAME`, and `DESCRIPTION` columns. Results are ordered by relevance, but the scores are not printed. When you search several endpoints, each failed endpoint prints `warning: <marketplace>: <error>` and appears under `errors` in JSON. The command fails only when no endpoint returned results and at least one endpoint reported an error. When you select a single endpoint with `--marketplace` and it fails, the command returns that error.

## `install`

Adds selections to one writable config layer, then syncs harness output.

You can select a named resource from a configured Git or directory source, or from `--from <registry>` with optional `--ref` and `--path`. In a v4 destination, an ad hoc registry becomes a named v4 `git` or `directory` source. A relative directory stays relative in repository and personal-local configs, so committed sources keep working on other machines. Installing every resource from an ad hoc registry into a v4 destination declares the source with an `all: true` resource group. `install` edits both v4 and legacy (v2 or v3) destinations. A change to a legacy destination prints a `config.legacy-deprecated` warning. If an edit is refused, your configuration stays unchanged. See [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

```text
agent-marketplace install <resource-name>... [flags]
agent-marketplace install <collection-name> [flags]
agent-marketplace install <marketplace-endpoint> [--scope user|repo|local]
agent-marketplace install <marketplace-locator> [flags]
agent-marketplace install --marketplace <alias-or-url> <resource-locator> [flags]
agent-marketplace install --tag <tag> [flags]
agent-marketplace install --all [flags]
agent-marketplace install --from <registry> [<resource-name>...] [flags]
```

`install` accepts several resource names in one command, resolved from the configured registries or from a single `--from` source:

```text
agent-marketplace install frontend-design review
agent-marketplace install --from https://github.com/example/resources.git frontend-design review
```

The shared `--scope`, `--kind`, `--ref`, and `--path` options apply to every name. A repeated name is installed once, in the order you first gave it. If a requested name is missing, malformed, ambiguous, or not allowed in the target scope, the command fails before writing configuration, acquisition lock, or materialization changes. Marketplace endpoint registration and locators remain single-resource operations and cannot be combined with other names.

Marketplace [locators](../glossary.md#locator) have one of these forms:

```text
alias:registry/kind/name[@sha256:<64 lowercase hex>]
https://host/r/registry/kind/name[/revisions/sha256:<64 lowercase hex>]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | endpoint: `user`; resource: `repo` | Configuration layer: `repo`, `user`, or `local`. |
| `--kind` | string | no | `skill` where a kind is needed | Selects `skill`, `command`, `subagent`, `hook`, or `mcp`, and disambiguates a named registry resource. |
| `--tag` | string | no | none | Installs registry resources whose metadata contains the tag. Without `--kind`, at least one skill must carry the tag. Works only in `repo` scope (or with no `--scope`). Cannot be combined with a resource name, `--all`, `--from`, or a Marketplace locator. |
| `--all` | boolean | no | `false` | Installs every discovered registry resource, or only those of `--kind` when you give it. Works only in `repo` scope (or with no `--scope`). |
| `--registry` | string | no | all configured registries | Limits `--all` to one configured registry. Requires `--all`. Matches the URL the way `update` and `lock` do, so a `.git` suffix or a trailing slash makes no difference, and looks in every config layer. The same source declared in several layers counts as one. If the URL names entries with different refs or paths, the command refuses and lists them. |
| `--from` | string | no | configured registries | Installs directly from an ad hoc Git URL, GitHub web URL, or GitLab web URL. Without a resource name, installs the whole registry. |
| `--ref` | string | no | inferred or registry default | Selects a branch, tag, or commit for `--from`. Requires `--from`. |
| `--path` | string | no | inferred or registry root | Selects a subdirectory for `--from`. Requires `--from`. |
| `--marketplace` | alias or endpoint URL | no | locator prefix | Resolves a Marketplace resource locator against the selected endpoint. Valid only for one Marketplace resource. |
| `--as` | safe local name | no | resource name | Installs the resource under a different local name while keeping its Marketplace coordinate. |
| `--accept-risk` | boolean | no | `false` | Accepts security findings for the verified revision. |
| `--yes` | boolean | no | `false` | Same as `--accept-risk`, for automation. |
| `-v`, `--verbose` | boolean | no | `false` | Shows every collection member and the technical Marketplace preview: revisions, digests, acquisition details, and provenance (where the artifact came from). |

`--as`, `--accept-risk`, and `--yes` work only with a single Marketplace resource. With `--marketplace <url>`, pass the full resource URL as the locator. In the current release, a short `registry/kind/name` locator combined with an endpoint URL fails with an `invalid browser locator` error, so use the full URL or an alias. If you pass a GitHub or GitLab web URL that names a branch or tag as the resource name, the command stops and tells you to use `--from` instead.

Registering a Marketplace endpoint checks the endpoint's well-known identity and canonical URL, then prints `Added Marketplace <url> to <config path>`, or `Marketplace <url> is already configured in <config path>` when the same identity is already present in that scope or a broader one. In a v4 configuration, registering the endpoint adds a named `marketplace` source for it. Installing a resource from that endpoint adds an exact request to the source's `resources` group and records the verified revision in the lock file of the same layer.

A Marketplace install resolves and verifies the exact artifact before it changes any config. The default output summarizes the source, security findings, capabilities, and up to ten collection members. Add `--verbose` to see all members, the digest, license, provenance, and acquisition mode. Rejected and revoked resources cannot be installed.

When the verified artifact has security findings and standard input is a terminal, the command prints the preview and asks `Install despite these risks? [y/N]:`. Accepting installs the exact prepared revision. Without a terminal, pass `--accept-risk` or `--yes` to accept the findings for that one invocation. Capabilities are shown, but on their own they never require consent.

A successful Marketplace install prints a short confirmation:

```text
✓ Installed frontend-design
✓ Installed coderabbit-skills · 3 skills
```

Installs from configured Git or directory registries say where the selection went: `in this repo`, `user-wide`, or `locally`. For example, `Installed frontend-design in this repo` or `Installed collection web-development in this repo`. The line lists only resources that the resulting configuration selects. A tag or bundle install keeps any existing exclusion and names the resources it left out, for example `Not installed: review; the effective configuration does not select it (for example, an exclusion wins). Install by name to select explicitly`. Installing a resource by name clears its exclusion. Missing prerequisites are reported as warnings; `install` does not install them for you.

For configured Git registries, discovery uses the exact locked inventory of every layer that already has a valid lock. A registry can bootstrap from its branch or tag only when its own configuration layer has no lock. A missing lock in one layer does not make content that appeared after another layer's lock installable.

## `remove`

Removes an explicit selection, or records an exclusion when the resource was selected some other way (for example, by a collection or a registry default). It then syncs output. Required organization resources cannot be removed or excluded.

`--from` and `--ref` follow the same rule, so an explicit selection in the target config is dropped. If a selected collection also provides the resource, it stays installed through the collection until a second `remove` excludes it from the collection.

For hosted Marketplace pins, removing a direct resource clears its direct selection but keeps it if a collection still includes it. Removing a collection clears that collection and removes the members no longer kept by anything else. You can remove a resource installed with `--as` by either its local name or its canonical Marketplace name. A name that matches more than one direct pin is refused.

When the destination layer is v4, named removals and collection-member exclusions of Git or directory resources use the v4-aware editor, including resources declared by a legacy organization, repository, or local layer. Other legacy-only removal paths can still refuse a v4 destination; the refusal names the real config file. Hosted sources have further restrictions; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

```text
agent-marketplace remove <resource-name> [flags]
agent-marketplace remove --from <registry> [<resource-name>] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | inferred for a name; `repo` with `--from` | Configuration layer: `repo`, `user`, or `local`. Choosing a scope leaves selections in other scopes unchanged. |
| `--kind` | string | no | inferred | Selects `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection` when a name is ambiguous. |
| `--from` | string | no | configured registries | Targets an ad hoc registry entry. Without a resource name, removes or excludes the whole entry for the selected scope. |
| `--ref` | string | no | none | Narrows the registry entry selected by `--from`. Requires `--from`. |
| `--path` | string | no | none | Narrows the registry entry selected by `--from`. Requires `--from`. |

The output says what happened:

```text
Removed frontend-design
Removed collection web-development
Excluded frontend-design (provided by web-development)
Removed registry entry https://github.com/example/resources.git
Excluded all resources from https://github.com/example/resources.git (local scope)
```

When the command chose the user config on its own, it adds a line such as `(excluded in /home/me/.agents/config.yaml — applies across all repos)`.

## `lock`

Resolves remote Git requirements to exact commits and verified source manifests, and writes them to the [lockfile](../glossary.md#lockfile) owned by each config layer. With the default `all` scope it writes the user, repository, and personal-local locks. It writes the organization lock only when you select `--scope organization`.

```text
agent-marketplace lock [resource ...] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `all` | Lock scope: `user`, `repo`, `local`, `all`, or `organization`. With `--check`, `all` also checks the organization lock. |
| `--check` | boolean | no | `false` | Reports whether the selected locks would change, without writing. A needed change returns exit status 1 with `lock.change-required` (or `lock.version-upgrade-required` for a v1 lock). |
| `--upgrade` | boolean | no | `false` | Refreshes all or targeted Git resolutions to the current commit of their refs. |
| `--registry` | string | conditionally | none | Targets one configured Git registry during `--upgrade`. |
| `--ref` | string | no | none | Narrows `--registry` to a declared ref. |
| `--path` | string | no | none | Narrows `--registry` to a locked source root. |
| `--kind` | string | conditionally | none | Resource kind for named `--registry` upgrade targets. |

Resource names and the targeting flags (`--registry`, `--ref`, `--path`, `--kind`) require `--upgrade`. Names, `--ref`, `--path`, and `--kind` also require `--registry`, and names require `--kind`. Upgrading one resource upgrades its whole Git source, because the commit is the smallest unit the CLI fetches.

On success the command prints `Locks: <n> changed`.

`lock --check` reads the existing config, lock, and exact snapshot files. It does not recover pending transactions, contact remotes, or fill the cache. A fresh lock whose exact snapshot is missing from a cold cache reports `lock.snapshot-unavailable`. A stale or missing lock reports the needed change without fetching it. A writing run holds the shared exact-cache lease from before its first snapshot fetch or check until the config and lock are published, so `gc` cannot delete a new snapshot before the published lock references it.

The lockfile paths are `/etc/agent-marketplace/lock.yaml`, `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`. Commit the repository lock and keep the personal-local lock out of Git. Some organizations never deploy an organization lock, and the CLI supports that permanently. In that case online commands may fetch organization Git sources from their current ref, or resolve explicit Marketplace requests into a verified lock held only in memory, but they never create the organization lock. Offline Marketplace resolution needs a deployed lock. A present organization lock is exact: if it is malformed, untrusted, or mismatched, commands refuse to continue and change nothing. Plain directory registries are read live and never appear in these locks.

## `sync`

Resolves the selected resources from exact Git snapshots and brings native harness output in line with configuration. When the organization layer has no lock, its requirements are fetched from their current refs.

```text
agent-marketplace sync [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--check` | boolean | no | `false` | Computes lock and output [drift](../glossary.md#drift) without fixing either. Returns exit status 1 when anything is out of sync. |
| `--locked` | boolean | no | `false` | Requires fresh lockfiles and forbids config or lock writes. |
| `--offline` | boolean | no | `false` | Uses only trusted local Git and Marketplace caches and never contacts a remote endpoint; lock behavior is the same as `--locked`. Cached revocation data may be stale. |
| `--allow-revoked` | exact Marketplace revision; repeatable | no | none | Allows one revoked pin for this run: `marketplace/registry/kind/name@sha256:<64 lowercase hex>`. Cannot be combined with `--check`. |
| `--no-backup` | boolean | no | `false` | Skips the configured pre-sync backup of machine-scope harness directories. The mandatory safety backups of shared configuration files still happen. |
| `--strict` | boolean | no | `false` | Fails on unresolved selections ("selected but not found", "found but failed to load") and on unmet prerequisites. |
| `--prune-stale` | boolean | no | `false` | After catalogs load successfully, removes direct, non-required selections that no longer exist from configuration, then syncs again. Cannot be combined with `--locked` or `--offline`. |
| `-v`, `--verbose` | boolean | no | `false` | Prints affected files per target and malformed-resource details instead of only the summary. |

A normal sync prints counts for installed, removed, malformed, and failed resources:

```text
Installed 3  Removed 1  Malformed 0  Errors 0
```

The counts are colored on a terminal. Set `NO_COLOR`, `CLICOLOR=0`, or `TERM=dumb` to turn color off.

If `~/.agents/config.yaml` does not exist, a plain `sync` creates it the way `agent-marketplace init --harnesses factory,windsurf` would: a v4 config with only `apiVersion` and `harnesses: [factory, windsurf]`. An existing user config is never read or changed by this step, and `--check`, `--locked`, and `--offline` skip it.

With `--check`, an in-sync result prints no verdict and returns exit status 0. Drift prints a summary such as `sync: 2 out of sync (1 missing, 1 orphaned)`, lists the planned changes, and returns exit status 1. Materialization and configuration errors also return exit status 1, including a dry-run materialization failure, which is printed as an `error:` line alongside the run's warnings. Warning-severity materialization diagnostics are printed as `warning:` lines and do not affect the exit status. `--check` writes nothing: not config, locks, transaction state, or harness output. It does not recover pending transactions or fetch a cold exact snapshot, and it reports `lock.snapshot-unavailable` when snapshot files it needs are missing.

If an earlier write was interrupted and left a dependency transaction pending, `sync --locked`, `sync --offline`, `sync --check`, `lock --check`, `list`, and `status` fail with `dependency.recovery-required` before reading configuration or registries. They leave config, locks, recovery records, and permissions unchanged. Run a plain `sync` or `lock` (without those flags) to finish the recovery, then retry. A missing transaction-state directory needs no recovery, and these commands do not create it.

## `update`

Moves the selected Git locks to the current commit of their refs and syncs enabled resources. Without targeting flags it updates all writable scopes.

```text
agent-marketplace update [resource ...] [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--bump` | boolean | no | `false` | Rewrites eligible refs to newer matching tags before updating locks, in legacy `registries:` entries and in v4 `sources:`. A ref that an included config file declares is reported and left unchanged. |
| `--scope` | string | no | `all` | Update scope: `user`, `repo`, `local`, or `all`. `organization` is refused. |
| `--registry` | string | no | none | Targets one configured Git registry. |
| `--ref` | string | no | none | Narrows `--registry` to a declared ref. |
| `--path` | string | no | none | Narrows `--registry` to a locked source root. |
| `--kind` | string | conditionally | none | Resource kind for named registry targets. |
| `-v`, `--verbose` | boolean | no | `false` | Lists individual organization-managed Marketplace pins. |

Resource names, `--ref`, `--path`, and `--kind` require `--registry`, and names require `--kind`.

The first line counts the configured registry entries, Git or local directory, whether or not their revision changed. With `--bump`, it also reports how many refs were rewritten and names any ref it left alone because an included config file declares it. The command checks writable Marketplace mirror pins for newer usable revisions. It does not check or rewrite organization-managed pins. By default the output counts those pins on one line, and `--verbose` lists each one. Other skipped pins still show their reason. Marketplace values are printed quoted:

```text
Configured registry entries: 2, refs bumped: 1
Marketplace pins updated: 1
Organization-managed Marketplace pins: 15 (not checked for newer revisions; pins unchanged)
Use `agent-marketplace update --verbose` to list organization-managed pins.
Marketplace "https://marketplace.example.com" "source"/"skill"/"review": updated "sha256:..." -> "sha256:..."
```

## `status`

Reports the effective configuration, selected resources, target harnesses, output roots, registry refs, cache freshness, source layers, drift, exclusions, collisions, unmet requirements, and errors. It only reads; it never changes output.

```text
agent-marketplace status
```

The report prints these sections in this order. Sections other than `TARGET HARNESS SET`, `SYNCED RESOURCES`, and `DRIFT` appear only when they have content.

| Section | Contents |
|---|---|
| `MANAGED INSTALLATION` | Present when an organization config manages the installation: its path and validation result. |
| `TARGET HARNESS SET` | Effective harness list and the configuration layer that supplied it. |
| `EXACT REGISTRY LOCKS` | Source layer, canonical Git origin, declared ref, exact commit, and whether the cached snapshot is `available` or `missing`. |
| `SYNCED RESOURCES` | Resource kind, name, harness, source, collection origin (`VIA`), target path, registry, ref, and cache state. |
| `MARKETPLACE PINS` | Endpoint identity, registry, resource coordinate, installed name, exact revision and digest, acquisition mode, source layer, revocation and current state, cache state, and update availability. |
| `HARNESS TARGETS` | Target capabilities and resolved repository and machine roots. |
| `LOCALLY EXCLUDED` | Resources removed by exclusions in a legacy v2 or v3 `.agents/config.local.yaml`. Exclusions in a v4 local file do not appear here. |
| `LOCAL REGISTRIES` | Registry entries declared in repository-personal configuration. |
| `ORPHANED FILES` | Generated files that a sync would remove. |
| `NAME COLLISIONS` | Registry overlap and resource-name collision diagnostics. |
| `UNMET REQUIREMENTS` | Advisory [resource prerequisites](./resource-prerequisites.md) and install hints. |
| `WARNINGS` | Configuration, resolution, and materialization warnings, with a remediation hint where one exists. |
| `DRIFT` | `In sync`, the number of files to write and remove, or `Unknown (exact registry view incomplete)`. |
| `ERRORS` | Configuration, catalog, materialization, or drift-inspection failures. |

Fatal resolution and error-severity materialization diagnostics return exit status 1 after the report is printed; warning-severity diagnostics do not affect the exit status.

## Configuration authoring

The CLI reads v4 roots and uses their `sources` and `resources` model for discovery, status, sync, and output. A root must declare `apiVersion: agent-marketplace/v4`. Included fragments may omit `apiVersion` and inherit v4 from the root.

Each source declares its kind:

- `git` sources require `ref` and use the owning layer's exact Git lock.
- `directory` sources are read live and never get a Git lock. Relative paths in the repository `.agents/config.yaml` and `.agents/config.local.yaml` roots are resolved from the repository root. User and organization configs and included fragments resolve them from the directory of the file that declares them.
- `marketplace` sources require a matching verified layer lock for explicit requests.

Use the [configuration reference](configuration.md) to write source declarations and resource groups by hand. Named Git and directory selections also have v4-aware CLI and TUI editors; editors that cannot handle v4 refuse and change nothing. The CLI rejects unknown fields, mixed source kinds, invalid locators, and Marketplace lock evidence that does not match. Writable-layer locks can be generated online for explicit hosted requests. When the organization lock is intentionally absent, the CLI fetches and verifies organization Marketplace requests online without writing an organization lock. Offline acquisition without a lock, hosted update or rebind, dynamic-selector snapshots, and general hosted editing are not supported.

## `migrate`

Converts legacy user, repository, and repository-personal configuration to the v4 source/resource schema, where the old file can be represented in v4. Migration is transactional: it validates every selected file before writing any. If you decline or migration refuses, the v2/v3 files stay readable. It uses verified local Marketplace cache evidence and never uses the network. Use `--to v2` only for the legacy structural-remediation flow.

```text
agent-marketplace migrate [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--to` | string | no | `v4` | Target schema: `v4` or `v2`. |
| `--scope` | string | no | `all` | Writable layer for v4 migration: `all`, `user`, `repo`, or `local`. `--to v2` rejects it, because that flow always covers every layer. |
| `--dry-run` | boolean | no | `false` | Previews v4 migration without writing config or lock files. `--to v2` rejects it, because that flow always writes. |
| `--harnesses` | comma-separated strings | no | none | Target harnesses for the user config. Both migration paths write them only into a user config that has no list, accept a list equal to the existing one, and refuse a different one. V4 migration also refuses them when `--scope` leaves out the user config or no user config exists. Without a terminal, `--to v2` requires it when the user config needs a harness list. |

`--to v4` is accepted as an explicit spelling of the default. V4 migration refuses input that would lose information, include trees, and Marketplace entries without complete verified cache evidence, and it changes no files when it refuses. Migration does not promise to keep comments or formatting. Git and directory-only conversion leaves locks unchanged. A Marketplace entry with verified evidence creates or upgrades the owning layer lock to `agent-marketplace-lock/v2`, published together with the migrated config in one transaction. Organization configuration is never edited.

The output names each migrated file, for example `Migrated user personal config: <path>` (or `Would migrate ...` with `--dry-run`). When nothing needs migrating, it prints `No legacy config files require v4 migration`.

## `convert`

Reads one harness layout and writes an equivalent layout for another harness. It does not edit Agent Marketplace configuration.

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
| `--sync` | boolean | no | `false` | Uses sync write mode and deletes stale output files. Without it, conversion overwrites files and deletes nothing. |
| `--dry-run` | boolean | no | `false` | Reports planned writes without changing output files. |

The output names the source, target, output directory, and operation counts:

```text
Converted claude -> factory into plugin-factory
Written 4, Skipped 0, Removed 0, Diagnostics 0
```

A partial conversion writes the resources that converted completely, prints `skipped <kind> "<name>" (<codes>)` for each skipped resource, skips pruning (`pruning was skipped because the conversion was partial`), and returns exit status 1. The command resolves source and output paths, including existing symlinks, and refuses when they are equal or one contains the other. It checks this before reading or changing either tree, including with `--sync`.

Codex conversion refuses a destination that already contains shared `.codex/hooks.json` or `.codex/config.toml` files. Devin conversion also refuses a destination that already contains `mcp_config.json`, `hooks.v1.json`, or `config.json`, instead of merging into it. For both targets the command stages every file, validates the staged shared files, publishes them, and on failure rolls back only the files that this run published. One-shot conversion creates no [ownership state](../glossary.md#ownership-state) and never takes ownership of existing entries. `sync` does not take ownership of pre-existing entries either. Only an explicit `doctor --repair` can, and only when an entry's bytes exactly match the value sync would generate. Missing or corrupt ownership state makes sync refuse to change shared entries.

## `doctor`

Reports repository detection, configuration diagnostics, harness capabilities and roots, Devin CLI compatibility, and unmet resource requirements.

```text
agent-marketplace doctor [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--repair` | boolean | no | `false` | Previews recovery actions for the ownership state of Devin and Codex shared configuration files and, on a terminal, asks you to confirm each one. |
| `--yes` | boolean | no | `false` | With `--repair`, adopts entries that exactly match the desired value without prompting. It never removes or replaces an entry. |

`doctor` detects a locally installed Devin CLI and reports whether it meets the 3000.3 compatibility floor. It recommends selecting the `devin` harness when a Devin CLI is installed but no harness list contains it. `sync` never needs a Devin binary, so none of these findings blocks it.

`--repair` covers `hooks.v1.json`, `config.json#hooks`, `mcp_config.json`, `.codex/hooks.json`, and `.codex/config.toml`. It sorts candidates into three groups: exact matches it can adopt, orphans it has evidence for (entries a completed sync wrote that are no longer wanted), and unmanaged entries whose key matches a selected resource but whose value differs. It lists every file it inspected, including files with nothing to repair, and it never edits harness lists. Without a terminal, it only previews, unless `--yes` can do byte-identical adoption. Removing an orphan or replacing a conflicting entry always needs a per-entry confirmation on a terminal, and the command takes a fresh backup first. `--yes` without `--repair` is an error.

Compatibility findings do not block `sync`. Repair failures, corrupt ownership state, and unparseable shared files return exit status 1.

## `backup`

Manages backups of machine-scope harness output. Backups live in `~/.agent-marketplace/backups/`.

```text
agent-marketplace backup <command>
```

### `backup create`

Creates an on-demand backup of the machine paths Agent Marketplace currently manages, then applies the configured backup rotation.

```text
agent-marketplace backup create
```

On success it prints `Backup created for: <targets>`. When backups are disabled in config, it prints `Backup is disabled in config; skipping.` and exits successfully. When no machine paths are managed, it prints `No managed paths to back up.` If configuration cannot be resolved, it prints each error and exits with status 1.

### `backup list`

Prints `Backups directory: <path>`, then the backup timestamps grouped by harness target. With no backups, it prints `No backups found.`

```text
agent-marketplace backup list
```

### `backup restore`

Copies the latest backup over the selected machine harness root. Files that are not in the backup stay in place.

```text
agent-marketplace backup restore --target <harness>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `-t`, `--target` | string | yes | none | Harness target: `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, or `windsurf`. |

On success it prints `Overlay-restored <target> from backup <timestamp>`. The command refuses a target root that is not a directory or is a symbolic link, and creates the root when it does not exist.

## `gc`

Removes cache entries that are no longer reachable from the validated configuration and active locks in the current context: the organization, user, current repository, and personal-local layers, including their includes. This covers mutable and exact Git cache entries and unreferenced hosted Marketplace artifacts.

What `gc` keeps:

- Exact Git snapshots referenced by a locked source and commit.
- Git object stores whose canonical source URL appears in a lock or in configuration, including organization sources with no organization lock.
- Marketplace blobs, manifests, revision metadata, and index-only occurrence snapshots referenced by exact configured pins.
- Protected derived-cache namespaces and persistent coordination locks.

It removes unreferenced object stores and abandoned staging directories.

The cache is shared by all your repositories, but `gc` only looks at the current one. Running it in repository A can delete a source that only repository B uses. B then has to fetch it again, which fails offline or when the remote no longer has the locked commit. Run `--dry-run` first to see what would be deleted.

`gc` first finishes any pending dependency-transaction recovery, holding the shared exact-cache lease while it publishes the recovered config and lock state. It then takes the exclusive exact-cache lease and reloads every applicable configuration layer and lock before deciding what is reachable. Missing required locks, malformed configuration or locks, and untrusted locks block deletion. A present organization lock must also match its configuration.

```text
agent-marketplace gc [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--dry-run` | boolean | no | `false` | Lists cache entries that would be removed without deleting them. |

The command prints one line per removed entry, followed by two totals:

```text
Removed 2 cache entries
Removed 1 marketplace artifact directories
```

With `--dry-run`, each line starts with `Would remove` instead.

## `serve`

Runs a hosted Marketplace from a separate [Marketplace operator configuration](marketplace-operator-configuration.md).

```text
agent-marketplace serve --config <path> [--role api|worker|all]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--config` | path | yes | none | Marketplace operator YAML file. |
| `--role` | string | no | `all` | `api` runs the public and operations listeners, `worker` runs background indexing, and `all` runs both. |

At startup the command opens the configured stores, makes sure the Marketplace instance exists, and applies configured revocations before either role reports ready. It runs until it is cancelled or a role fails.

### `serve reindex`

Queues and processes refresh work outside the scheduler.

```text
agent-marketplace serve reindex [registry-slug-or-id] --config <path>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--config` | path | yes | none | Marketplace operator YAML file. |

With no registry argument, the command reindexes every configured registry. With one slug or ID, it reindexes only that registry. It applies revocations before fetching and checks store readiness after processing.

### `serve reset`

Resets one registry's generated catalog during a maintenance window. It accepts exactly one registry slug or ID; there is no all-registry default.

```text
agent-marketplace serve reset <registry-slug-or-id> --config <path> --dry-run
agent-marketplace serve reset <registry-slug-or-id> --config <path> --confirm-maintenance
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--config` | path | yes | none | Marketplace operator YAML file. |
| `--dry-run` | boolean | no | `false` | Reports affected row counts without resetting catalog data. Takes precedence over `--confirm-maintenance`. |
| `--confirm-maintenance` | boolean | for reset | `false` | Confirms all API/workers are stopped, metadata is backed up, and controllable shared caches are invalidated. |

Reset deletes the selected registry's resource revisions, assessments, collections, occurrence and publication history, current catalog pointers, and blob-ownership references in one metadata transaction. Its refresh status becomes unknown. Registry settings, managed configuration and administrative history, policies, revocations, other registries, and all blob bytes remain intact. Shared blobs are not deleted. Database-managed mode resolves the selector from managed configuration rather than stale operator YAML.

The command refuses a destructive reset while any job holds a live lease, but cannot verify external process shutdown or cache invalidation. The registry is unavailable until a subsequent `serve reindex <registry-slug-or-id> --config <path>` succeeds; reindexing is not automatic. References from other registries may be unavailable until rebuilding completes, and historical revisions absent from the current source are not recovered by reindexing. Existing locks and cached revision metadata may need refreshing. Reset does not make older clients compatible with newer artifact contents.

Both modes open the configured stores, including normal schema initialization. Output begins with `Would reset catalog` or `Reset catalog`, followed by per-table row counts. The command returns nonzero on invalid arguments, missing confirmation, an unknown registry, live job leases, or a storage failure; a failed metadata transaction rolls back the reset.

### `serve adopt`

Adopts catalog settings during a controlled maintenance window and records the database as the configuration authority.

```text
agent-marketplace serve adopt --config <path> --actor <id> --reason <text> --idempotency-key <key> --confirm-maintenance
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--actor` | string | yes | none | Operator identity recorded in the maintenance audit. |
| `--config` | path | yes | none | Path to the Marketplace operator config file. |
| `--confirm-maintenance` | boolean | yes | `false` | Confirms all API and worker processes are stopped, backups exist, and shared caches are invalidated. The command refuses to run without it. |
| `--idempotency-key` | string | yes | none | Stable key reused when retrying this adoption. |
| `--reason` | string | yes | none | Reason recorded in the adoption audit. |

Before running adoption, stop every API and worker process and back up the database and blob storage. The command does not check either. The configuration file must set `management.mode: database`. On success it prints `Adopted marketplace configuration at revision <n>.`

## `deinit`

Removes the files Agent Marketplace manages for one scope. User scope also removes orphaned registry caches, but keeps the machine output that organization requirements still resolve to. A present organization lock is read exactly, and an invalid lock stops the teardown. When the organization lock is absent, a normal `deinit` may fetch the organization registry to keep its output and cache. `--dry-run` never fetches, so an uncached, unlocked organization registry produces a warning. Configuration files stay unless you pass `--purge-config`.

```text
agent-marketplace deinit [flags]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--scope` | string | no | `user` | Teardown scope: `user`, `repo`, or `local`. `repo` and `local` both remove repository-scoped output. |
| `--purge-config` | boolean | no | `false` | Also deletes the scope's configuration file and the lockfile it owns, including an orphaned lock. Other layers and included fragments stay. Transaction coordination files and unrelated recovery records stay. A pending transaction that touches these files blocks the purge until it is recovered. |
| `--dry-run` | boolean | no | `false` | Reports the output, caches, configuration files, and lockfiles that would be removed, without deleting them. |

The output reports `Removed <n> managed artifact(s)` (or `Would remove ...` with `--dry-run`), or `No managed artifacts to remove`, followed by each removed cache and purged file.

Unreadable configuration does not stop the teardown. The command prints a warning, assumes the default materialization mode, and notes that output created in another mode may remain.

## `telemetry`

Reads and changes your telemetry preference and local telemetry state. When an organization config sets a telemetry level, that level applies instead of your preference, and the organization can also lock the endpoint. See the [telemetry reference](telemetry.md).

```text
agent-marketplace telemetry <command>
```

### `telemetry status`

Prints the effective level and its source, the endpoint hostname, header references, retention policy, community-reporting state, whether the installation is managed, the installation ID, queue statistics, the number of retained diagnostic payloads, the last delivery failure, and any fields locked by the organization.

```text
agent-marketplace telemetry status
```

Header values are never printed.

### `telemetry enable`

Sets your telemetry level, and optionally a user-managed OTLP/HTTP endpoint. The user config must already exist; otherwise the command tells you to run `agent-marketplace init` first.

```text
agent-marketplace telemetry enable --level <level> [--endpoint <url>]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--level` | string | yes | none | `basic` or `detailed`. `off` is rejected; use `telemetry disable` instead. |
| `--endpoint` | URL string | no | existing effective endpoint | Stores a user endpoint. Refused when organization policy sets the endpoint. |

A level lower than the organization's level is rejected. On success the command prints `Telemetry enabled at "<level>"`.

### `telemetry disable`

Sets your telemetry preference to `off` and prints `Telemetry disabled for user preference.`

```text
agent-marketplace telemetry disable
```

If organization policy keeps telemetry on, the command also prints `Organization policy keeps telemetry active at "<level>".`

### `telemetry preview`

Writes a representative payload to a local file without sending it.

```text
agent-marketplace telemetry preview --level <level>
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--level` | string | yes | none | Preview level: `basic`, `detailed`, or `diagnostic`. |

A diagnostic preview asks for confirmation on a terminal. Without a terminal, it requires the global `--diagnostic-accept-sensitive-data` flag. The output prints `Preview payload written to <path>` with the level, endpoint host, retention policy, and header references.

### `telemetry purge`

Removes queued telemetry records and retained diagnostic payloads, then prints how many of each it removed. It does not reset the installation ID or change telemetry preferences.

```text
agent-marketplace telemetry purge
```

### `telemetry reset-id`

Replaces the installation ID and purges pending telemetry tied to the old ID. The output prints `Installation ID reset. New ID: <id>`.

```text
agent-marketplace telemetry reset-id
```

## `tui`

Starts the interactive terminal UI for browsing Git registries and hosted Marketplaces, editing selections, choosing harnesses, and syncing. See the [terminal UI reference](tui.md) for keys and screens.

```text
agent-marketplace tui
```

The command finds the repository root from the current directory and uses your home directory for machine-scope configuration and output. It shows configured resources before any network discovery, loads up to 50 results from each Marketplace, and starts a Marketplace search 250 milliseconds after you stop typing. Discovery results stay fresh for five minutes and can be shown as stale results for up to 24 hours when an endpoint fails.

Marketplace installation verifies the exact revision before saving it. Security findings show the verified preview and require confirmation; capability metadata alone does not. Blocked and revoked resources stay visible but cannot be installed.

## `completion`

Generates shell completion scripts with Cobra's built-in completion commands.

```text
agent-marketplace completion bash [--no-descriptions]
agent-marketplace completion zsh [--no-descriptions]
agent-marketplace completion fish [--no-descriptions]
agent-marketplace completion powershell [--no-descriptions]
```

| Flag | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `--no-descriptions` | boolean | no | `false` | Leaves command and flag descriptions out of the generated completions. |

The script is written to standard output.

## Exit statuses

| Status | Condition |
|---:|---|
| `0` | The command succeeded. Warnings, such as a missing target set for `list`, unmet advisory [prerequisites](./resource-prerequisites.md), or non-blocking compatibility findings, do not change this status. |
| `1` | Argument validation, configuration loading, registry access, resolution, conversion, materialization, repair, backup, or filesystem work returned an error. |
| `1` | `sync --check` found drift, or `lock --check` found a lock that would change. |
| `1` | `sync --strict` found a selected resource that was missing or malformed, or an unmet prerequisite. |

The CLI defines no other numeric statuses. Argument and runtime errors are printed to standard error. Reports and normal results are printed to standard output.
