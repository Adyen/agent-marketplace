# How to install a plugin

Select a plugin package from a Git or directory source, inspect what it
contains, choose how it reaches your harness, and sync it with the rest of
your repository's resources.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A repository with `.agents/config.yaml`.
- A Git or directory source containing a plugin package or native plugin
  catalog.

## Before you commit a plugin source

Every teammate and CI job that reads the repository configuration must use
Agent Marketplace v5.2.0 or newer before you commit a plugin source. Earlier
clients fail closed on the `format` field or plugin selection: v5.1.0 reports
`v4.unknown-field` for `format`, `v4.locator` for `plugin/<name>`, and
`v4.resource-item` for a structured `{kind: plugin, name: ...}` entry.

The source's `format` tells Agent Marketplace how to read it. For example,
`claude-marketplace` reads a Claude Code marketplace catalog, while
`claude-plugin` reads a Claude plugin package. The
[plugin support reference](../reference/plugin-support.md) shows which
package formats each harness accepts.

## 1. Declare a format source and select a plugin

For a native catalog stored in Git, declare its catalog format on the source
and select a plugin by name:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  team-plugins:
    git: https://github.com/example-org/plugins.git
    ref: v1.4.0
    format: claude-marketplace
resources:
  team-plugins:
    include:
      - plugin/review-kit:
          delivery: auto
          exclude: [skill/legacy-review]
```

You can also declare `format` on a directory source. A format source
discovers plugins; a Git or directory source without `format` keeps its
existing loose resource discovery. Directory sources stay live and are not
pinned in an acquisition lock. `format` is not a Marketplace source option,
and it cannot be combined with `source_harness`.

If a plugin name cannot be written as a locator, use the structured form.
For example, an Agent Plugins package named `1password.tools` can be selected
from a source with `format: agent-plugin`:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  portable:
    directory: ./vendor/onepassword
    format: agent-plugin
resources:
  portable:
    include:
      - kind: plugin
        name: "1password.tools"
```

See the [configuration reference](../reference/configuration.md) for the
complete source and selection fields.

## 2. Choose members and delivery

Use `exclude` with exact `kind/name` member locators to leave a member out of
the selected package. An exclusion that does not name a member fails with
`plugin.member.unknown-exclusion`. Do not select a member such as
`skill/review` directly from a format source. Member locators are scoped
inside their plugin, and direct selection is refused with
`plugin.member-direct-selection`.

`delivery` is a plugin-request option:

- `auto` (the default) uses native delivery only when the harness catalog
  marks it verified; otherwise it decomposes the plugin into ordinary
  resources.
- `decompose` always installs the members as ordinary harness resources.
- `native` requests registration as a plugin in the harness. It does not
  fall back to decomposition when native delivery is unverified.

Native delivery is unverified for all seven harnesses, so `auto`
chooses decomposed delivery. An explicit `delivery: native` fails with
`plugin.delivery.native-unverified` and writes no members. The generated
[support matrix](../reference/plugin-support.md) records the current catalog
evidence. `delivery` and `exclude` are not accepted on
non-plugin requests; plugin requests also cannot use `as`.

## 3. Inspect before installing

List plugins discovered from configured sources:

```bash
agent-marketplace list --kind plugin
```

Inspect one package and its members, target delivery, losses, and blockers
without installing it or changing files:

```bash
agent-marketplace inspect plugin/review-kit
```

Plugins that declare dependencies can be inspected, but installation is
refused with `plugin.format.dependencies-unsupported`.

## 4. Export a locked plugin

Export writes a locked plugin as an Agent Plugins 1.0.0 package:

```bash
agent-marketplace export plugin/review-kit \
  --format agent-plugin \
  --out ./exported/review-kit
```

Export requires a locked plugin, so use a Git-backed format source; a live
directory source does not provide a lock. The output path must be absent or
empty. Export reports losses and writes nothing if the package cannot be
represented without loss. `--allow-loss` can permit non-security omissions,
but it cannot discard a security restriction. The
[CLI reference](../reference/cli.md) lists the flags and failure behavior.

## 5. Sync and check status

Sync resolves the selected plugin, records Git-backed sources in the layer's
lock, and delivers it to the configured harnesses:

```bash
agent-marketplace sync
agent-marketplace status
```

Status reports the plugin's locked and installed revisions separately. To
check lock and installed-output drift without writing:

```bash
agent-marketplace sync --check
```

`sync --check` exits with status `1` when there is drift and does not change
configuration, locks, or harness output.

## 6. Remove a plugin

Remove the direct plugin selection:

```bash
agent-marketplace remove plugin/review-kit
```

If another selected resource, such as a collection, still selects that
plugin, it remains installed until that selection is removed too. Removal
cleans up only Agent Marketplace-owned plugin output. If the plugin has data,
removal keeps it and reports its retained path. See the
[lockfiles reference](../reference/lockfiles.md) for the records used to
reproduce a plugin selection.

For the complete diagnostic catalog, see the
[diagnostics reference](../reference/diagnostics.md).
