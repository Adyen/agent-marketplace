# Convert configuration before upgrading

The v4-only CLI requires exact `apiVersion: agent-marketplace/v4` in every existing consumer root: organization, user-personal, repository, and personal-local. It has no `migrate` command, startup conversion prompt, TUI migration modal, or legacy reader/editor. Convert your complete installation **before replacing the older binary**.

This page keeps its original URL for upgrade guidance. The migration commands below belong to an **older released binary that provides the migrator**, not the new CLI. The harness layout [`convert`](./convert-a-harness-layout.md) command is independent and stays in the new CLI.

## 1. Inventory and back up every layer

Check each applicable root and its recursive local-file `include` tree:

| Layer | Root | Who converts it |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | Its administrator, externally |
| User-personal | `~/.agents/config.yaml` | You, using the older released migrator |
| Repository | `<repo>/.agents/config.yaml` | The team, using the older released migrator |
| Personal-local | `<repo>/.agents/config.local.yaml` | You, using the older released migrator |

Repeat the repository and local check for each checkout or independently configured subtree you use. Back up authored configs, included fragments, and their layer-owned locks, and keep a compatible earlier binary for rollback. Don't confuse the consumer organization root with the Marketplace operator configuration: `agent-marketplace.io/marketplace/v1` remains supported by `serve`.

An absent layer keeps its normal first-run behavior. An existing empty or versionless file is not absent, and a personal-local root has no versionless exception. Every root must declare its own exact version. A fragment cannot supply the root's version.

## 2. Use the older released migrator

Keep or obtain an older released binary whose help exposes `migrate --to v4`. Store it apart from the new binary and verify the release artifact. In the examples, `./agent-marketplace-older` is that retained executable. The v4-only release does not ship it.

From the repository, inspect the old binary's help and preview the conversion:

```bash
./agent-marketplace-older migrate --help
./agent-marketplace-older migrate --to v4 --scope all --dry-run
```

Review the planned changes, then run the older migrator:

```bash
./agent-marketplace-older migrate --to v4 --scope all
```

Use `--scope user`, `--scope repo`, or `--scope local` to target one writable root, if the older release supports those flags. The migrator never converts the externally owned organization file. Ask its administrator to convert and deploy that root and its fragments before you upgrade.

### Conversion is not unconditional

The older migrator can refuse a root that uses `include`, unrepresentable or lossy legacy input, or exact Marketplace pins without complete verified local cache evidence. Follow that release's diagnostics and resolve the refusal before you upgrade. Include trees may need a reviewed manual conversion of the full source/resource structure. Don't flatten away include authority, relative path bases, exclusions, required policy, or layer ownership just to make a converter accept them.

Exact Marketplace pins need verified revision and artifact or collection evidence. If evidence is missing, use the older compatible client's supported acquisition workflow and retry its migrator. Don't fabricate trust data, digests, or lock records. The older migrator makes no network requests. For Git and directory-only conversion, it leaves locks unchanged. Conversion of evidenced Marketplace pins can publish a v2 lock with the converted config.

Versionless or v1 input may need manual structural work under the older release's rules before its migrator can handle it. **Merely changing or adding `apiVersion` is not conversion.** Legacy `registries`, top-level installation lists, and `marketplaces` must become valid v4 `sources` and source-bound `resources`, keeping the intended selection and scope. A v4-validated root or included fragment that still has legacy installation keys fails with `v4.legacy-key`.

## 3. Verify the complete v4 installation

A root selects resources through named sources:

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

Git sources require `ref`. Directory sources are live and do not use Git locks. Relative directory paths in repository and personal-local roots resolve from the repository root. In user and organization files and included fragments, they resolve from the declaring file's directory. See the [configuration reference](../reference/configuration.md) for fields and [layer restrictions](../reference/configuration-layers.md) before you publish converted files.

An explicitly included fragment under a validated v4 root may omit `apiVersion` or declare exactly `agent-marketplace/v4`. An explicit v1, v2, v3, unknown, or invalid fragment version refuses the include tree. Fragments keep their layer restrictions, merge order, source bindings, and authority. Supported edits target the owning root, not the fragment, and locks stay in the logical layer.

Check that the externally deployed organization root is also v4. With a v4-capable client, synchronize and inspect the intended selections, harnesses, and lock and output state before you replace the old binary:

```bash
./agent-marketplace-older sync
./agent-marketplace-older status
./agent-marketplace-older lock --check
./agent-marketplace-older sync --check
```

Review and commit the shared v4 configuration and repository lock together. Keep personal-local files and generated harness output out of Git. Keep backups for rollback. Rolling back means using a compatible older binary with the backed-up configuration. It does not mean asking the new binary to write legacy syntax.

Normal v4 installs and removals, bulk, tag, and ad-hoc selections, TUI toggles, harness edits, and `update --bump` all stay supported. Conversion does not enable unsupported hosted features. Offline acquisition without an organization lock, hosted rebind, Marketplace dynamic-selector snapshots, and other unsupported runtime options still refuse.

## 4. Upgrade and check

After every applicable root and include tree is valid v4, [install the new CLI](../install.md), then run:

```bash
agent-marketplace status
agent-marketplace sync --locked
agent-marketplace sync --check
```

Consumer configuration versioning is independent of acquisition-lock v1 reads and v2 writes, historical source-revision verification, API and artifact versions, and native hook and harness schemas. Don't rewrite those formats just because their version number is not v4.

## Troubleshooting a refusal

- `v4.api-version-required`: the diagnostic's root file omits `apiVersion`. Convert and validate its complete structure with the older workflow. Don't just add a sentinel to legacy fields.
- `v4.api-version`: the diagnostic's root or explicitly versioned fragment has an unsupported or invalid value. The string must match `agent-marketplace/v4` exactly, with no whitespace or case variations.
- `v4.legacy-key`: the version is v4 but old installation keys remain. Complete the source/resource conversion.
- `config.edit.include-owned`: the requested edit belongs to a fragment. Review the fragment directly. Normal CLI edits do not rewrite it.
- `v4.layer-field`: move fields to a permitted layer, for example `harnesses` out of personal-local configuration.
- `v4.marketplace-runtime-unsupported`: use explicit supported Marketplace requests rather than dynamic selectors or unsupported group options.
- `dependency.recovery-required`: a read-only command found an earlier interrupted write and will not replay it. A command that can recover finishes only that earlier write before it checks the configuration. If recovery is blocked, its records are kept.

Format diagnostics name the file and its `apiVersion` field, with the line when known. A refused command downloads nothing and writes no config, lock, or harness files. Existing files and their metadata are untouched, and no missing config file or lock is created. It may still write logs or finish an earlier interrupted write. The new CLI and TUI never offer to convert anything.
