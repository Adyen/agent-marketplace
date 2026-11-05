## Context

`plugin-decomposed-delivery` delivers plugin instances as decomposed members
from immutable package generations, one dependency transaction participant
per instance, and keeps `delivery: native` pending. `plugin-policy-catalogs`
ships one harness catalog per target with `delivery.native.status`, which is
`unverified` everywhere until a probe passes. Claude Code and Codex already
merge owned hook and MCP entries into shared files (`settings.json`,
`config.toml`) through `NativeLayout` aggregates and ownership state.

## Goals / Non-Goals

**Goals:** per-request delivery resolution, native registration through
owned entries for Claude Code and Codex, `reload-required` reporting, and
recoverable delivery transitions, proven with a test-only verified catalog.

**Non-Goals:** native probes and flipping any shipped catalog to verified,
Factory Droid registration (its shape is settled by its probe), git-sourced
native catalog entries, changes to removal, plugin data, generation
collection, or export, and documentation.

## Decisions

### D1: Delivery resolution

`auto` resolves per harness target to native exactly when that target's
catalog declares `delivery.native.status: verified`, and to decompose
otherwise. `native` to an unverified target fails the instance with
`plugin.delivery.native-unverified` and writes nothing for it. Sync reports
the resolved delivery per target.

### D2: Native registration

Native delivery publishes the generation into a native plugin catalog
directory owned by Agent Marketplace under the state root, then writes owned
entries through new `NativeLayout` registration members:

| Harness target | Owned entries |
|---|---|
| Claude Code | `settings.json` `extraKnownMarketplaces.<am-catalog>` and `enabledPlugins["<plugin>@<am-catalog>"]` |
| Codex | `config.toml` `[marketplaces.<am-catalog>]` (local source) and `[plugins."<plugin>@<am-catalog>"]` |

The catalog name is derived from the scope and is the same for every plugin
of that scope. Entries share each file with hooks and MCP servers and are
planned in one combined mutation from one read, with handwritten keys,
order, and comments preserved. Ownership state is the only proof of
ownership: an entry matching the naming but absent from ownership state is
left unchanged and reported as a collision. Codex trust is never written.
A target without registration members refuses native delivery and writes
nothing for that instance.

### D3: Reload reporting

The harness catalog's native delivery gains a closed-enum refresh field.
When a native generation changes for a target whose catalog declares that
refresh requires a restart, sync and status report `reload-required` for the
instance. Shipped catalogs keep the default.

### D4: Delivery transitions

Changing an instance between native and decomposed delivery stages the new
delivery first, then retires only proven owned outputs and registrations of
the old delivery, in one dependency transaction sequence. An interrupted
switch is completed by the next recovery-capable command; read-only commands
refuse pending work without replay.

### D5: Test-only verified catalog

Tests force native delivery with a verified harness catalog that is
available only in test and `e2e` builds. Production builds cannot load it,
and every shipped catalog stays `unverified`.

## Risks / Trade-offs

- Registration shapes are candidates until each harness probe passes; the
  shipped catalogs keep native delivery unreachable until then.
- Claude registration shares `settings.json` with native hooks; the
  released-reader gate applies before native Claude writes ship.
