# Convert configuration before upgrading

The v4-only CLI requires exact `apiVersion: agent-marketplace/v4` in every existing consumer root: organization, user-personal, repository, and personal-local. It has no `migrate` command, startup conversion prompt, TUI migration modal, or legacy reader/editor. Convert your complete installation **before replacing the older binary**.

This page retains its original URL for upgrade guidance. All migration commands below belong to an **older released binary that provides the migrator**, not the new CLI. Harness layout [`convert`](./convert-a-harness-layout.md) is independent and remains available in the new CLI.

## 1. Inventory and back up every layer

Check each applicable root and its recursive local-file `include` tree:

| Layer | Root | Who converts it |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | Its administrator, externally |
| User-personal | `~/.agents/config.yaml` | You, using the older released migrator |
| Repository | `<repo>/.agents/config.yaml` | The team, using the older released migrator |
| Personal-local | `<repo>/.agents/config.local.yaml` | You, using the older released migrator |

Repeat the repository/local check for each checkout or independently configured subtree you use. Back up authored configs, included fragments, and their layer-owned locks, and retain a compatible earlier binary for rollback. Do not confuse the consumer organization root with the Marketplace operator configuration: `agent-marketplace.io/marketplace/v1` remains supported by `serve`.

An absent layer keeps its normal first-run behavior. An existing empty or versionless file is not absent, and a personal-local root no longer has a versionless exception. Every root must declare its own exact version; a fragment cannot supply the root's version.

## 2. Use the older released migrator

Keep or obtain an older released binary whose help exposes `migrate --to v4`. Store it separately from the new binary and verify the release artifact. In the examples, `./agent-marketplace-older` is that separately retained executable, not a command shipped by the v4-only release.

From the relevant repository, inspect the old binary's help and preview conversion:

```bash
./agent-marketplace-older migrate --help
./agent-marketplace-older migrate --to v4 --scope all --dry-run
```

Review the planned changes, then run the older migrator:

```bash
./agent-marketplace-older migrate --to v4 --scope all
```

Use `--scope user`, `--scope repo`, or `--scope local` to target one writable root when the older release supports those flags. The migrator never converts the externally owned organization file; arrange for its administrator to convert and deploy that root and its fragments before you upgrade.

### Conversion is not unconditional

The older migrator can refuse a root that uses `include`, unrepresentable or lossy legacy input, or exact Marketplace pins without complete verified local cache evidence. Follow that release's diagnostics and resolve the refusal before upgrading. Include trees may require a reviewed manual conversion of the full source/resource structure; do not flatten away include authority, relative path bases, exclusions, required policy, or layer ownership merely to make a converter accept them.

Exact Marketplace pins require verified revision and artifact or collection evidence. If evidence is missing, use the older compatible client's supported acquisition workflow and retry its migrator. Do not fabricate trust data, digests, or lock records. The older migrator itself makes no network requests. For representable Git/directory-only conversion it leaves locks unchanged; conversion of evidenced Marketplace pins can publish a v2 lock with the converted config.

Versionless or v1 input may require manual structural work under the older release's rules before its migrator can handle it. **Merely changing or adding `apiVersion` is not conversion.** Legacy `registries`, top-level installation lists, and `marketplaces` must become valid v4 `sources` and source-bound `resources`, while preserving the intended selection and scope. A v4-validated root or included fragment that still has legacy installation keys fails with `v4.legacy-key`.

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

Git sources require `ref`. Directory sources are live and do not use Git locks. Relative directory paths in repository and personal-local roots resolve from the repository root; in user/organization files and included fragments they resolve from the declaring file's directory. See the [configuration reference](../reference/configuration.md) for fields and [layer restrictions](../reference/configuration-layers.md) before publishing converted files.

An explicitly included fragment under a validated v4 root may omit `apiVersion` or declare exactly `agent-marketplace/v4`. An explicit v1/v2/v3, unknown, or invalid fragment version refuses the include tree. Fragments retain their layer restrictions, merge order, source bindings, and authority; supported edits target the owning root, not the fragment, and locks stay in the logical layer.

Check that the externally deployed organization root is also v4. With a compatible v4-capable client, synchronize and inspect the intended selections, harnesses, and lock/output state before replacing the old binary:

```bash
./agent-marketplace-older sync
./agent-marketplace-older status
./agent-marketplace-older lock --check
./agent-marketplace-older sync --check
```

Review and commit the shared v4 configuration and repository lock together. Keep personal-local files and generated harness output out of Git. Retain backups for rollback; rollback means using a compatible older binary with backed-up configuration, not asking the new binary to write legacy syntax.

Normal v4 installs/removals, bulk/tag/ad-hoc selections, TUI toggles, harness edits, and `update --bump` remain supported. Conversion does not enable unsupported hosted features: offline acquisition without an organization lock, hosted rebind, Marketplace dynamic-selector snapshots, or other unsupported runtime options still refuse.

## 4. Upgrade and check

After every applicable root and include tree is valid v4, [install the new CLI](../install.md), then run:

```bash
agent-marketplace status
agent-marketplace sync --locked
agent-marketplace sync --check
```

Consumer configuration versioning is independent of acquisition-lock v1 reads/v2 writes, historical source-revision verification, API/artifact versions, and native hook/harness schemas. Do not rewrite those formats just because their version number is not v4.

## Troubleshooting a refusal

- `v4.api-version-required`: the diagnostic's root file omits `apiVersion`. Convert and validate its complete structure with the older workflow; do not just add a sentinel to legacy fields.
- `v4.api-version`: the diagnostic's root or explicitly versioned fragment has an unsupported or invalid value. The string must match `agent-marketplace/v4` exactly, without whitespace or case variations.
- `v4.legacy-key`: the version is v4 but old installation keys remain. Complete the source/resource conversion.
- `config.edit.include-owned`: the requested edit belongs to a fragment. Review the fragment directly; normal CLI edits do not rewrite it.
- `v4.layer-field`: move fields to a permitted layer, for example `harnesses` out of personal-local configuration.
- `v4.marketplace-runtime-unsupported`: use explicit supported Marketplace requests rather than dynamic selectors or unsupported group options.
- `dependency.recovery-required`: a read-only mode found a pre-existing interrupted dependency transaction. It will not replay it. A recovery-capable command can complete only the already-admitted journaled writes before validating fresh configuration; blocked recovery retains its evidence.

Format diagnostics identify the offending authored file and `apiVersion` field, with line context when available. A refused new operation performs no acquisition, config/lock staging or publication, or harness writes/prunes, preserves existing bytes and metadata, and creates no missing writable root or lock. Ordinary operational logs and safe recovery of previously admitted journaled writes are exceptions to a blanket no-write claim. There is no conversion prompt to accept or decline in the new CLI or TUI.
