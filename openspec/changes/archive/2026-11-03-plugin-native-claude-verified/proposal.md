## Why

Plugin support (issue #6, v5.0.0) writes a Claude Code native registration,
but Claude native delivery is still `unverified`, so `auto` decomposes every
Claude plugin. CI probes with Claude Code 2.1.316, run in a trusted isolated
home, show the shipped user-scope registration loading every time: Claude
registers the marketplace, installs the plugin itself, lists it enabled, and
discovers its namespaced skill. At repository scope Claude registers the
marketplace but never installs or loads the plugin, whether the registration
sits in `.claude/settings.local.json` or `.claude/settings.json`, in headless
or interactive sessions. Native delivery can be verified at user scope now;
repository scope stays decomposed until the gap is explained.

## What Changes

- Plugin catalogs can declare native delivery status per scope. The Claude
  catalog declares native delivery verified at user scope with the
  `claude-native-plugin` probe and unverified at repository scope.
- `auto` delivers Claude plugins natively at user scope and decomposed at
  repository scope; explicit `delivery: native` at repository scope is refused
  with `plugin.delivery.native-unverified`.
- The Claude harness-compatibility job runs the probe. At user scope a
  pre-sync negative control confirms Claude does not list the plugin; after
  sync the probe confirms Agent Marketplace wrote none of Claude's
  installed-plugin records, known-marketplace records, plugin cache, or trust
  state, establishes the user's own workspace trust in its isolated home,
  waits in a headless session for Claude's own install record, and requires
  `claude plugin list` to report the plugin enabled and Claude initialization
  to list its namespaced skill. At repository scope it confirms decomposed
  delivery and no native registration.
- The plugin support reference shows native status per scope, and the Claude
  validation notes record the open repository-scope gap.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: Claude Code native delivery is verified at user scope by
  a Claude CLI probe; delivery resolution honors per-scope native status.
- `plugin-policy-catalogs`: native delivery status may be declared per scope.

## Impact

- `pkg/harness-compilation/pluginpolicy`: per-scope native status, the Claude
  catalog, and the probe identifier.
- Delivery resolution for plugin requests (scope-aware native verification).
- `tools/harness-compat`, `tools/catalog-docs`: defined probes and the
  generated reference.
- `e2e`: the Claude native plugin probe.
- `cmd/agent-marketplace`: plugin command tests.
- `docs/harness-validation/claude.md`, `site/docs/reference/plugin-support.md`.
- The registration entries, Factory Droid and Codex delivery are unchanged.
