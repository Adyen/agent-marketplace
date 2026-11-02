## Why

Plugin support (issue #6, v5.0.0) writes a Claude Code native registration,
but Claude native delivery is still `unverified`, so `auto` decomposes every
Claude plugin. Earlier CI probes with Claude Code 2.1.311 never saw the
registration load. Claude's documentation explains part of that result:
repository-scope `extraKnownMarketplaces` apply only in a workspace the user
trusted, and Claude registers declared marketplaces in the background after a
session starts. The earlier probe never established trust and observed only
headless sessions. A probe that reproduces what a real user does, without
Agent Marketplace writing any Claude-owned state, can verify the shipped
registration.

## What Changes

- The Claude harness-compatibility job runs a native plugin probe at user and
  repository scope. Before sync, a negative control confirms Claude does not
  list the plugin. After sync, the probe confirms Agent Marketplace wrote none
  of Claude's installed-plugin records, known-marketplace records, plugin
  cache, or trust state. It then establishes the user's own workspace trust
  in its isolated home, starts a Claude session that performs Claude's
  marketplace registration, and requires `claude plugin list` to report the
  plugin enabled and Claude initialization to list its namespaced skill.
- The Claude plugin catalog declares native delivery `verified` with that
  probe, so `auto` delivers Claude plugins natively and explicit
  `delivery: native` is accepted for Claude.
- Documentation states that a repository-scope Claude registration takes
  effect only in a workspace the user trusted, and that Agent Marketplace
  never writes Claude trust state.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: Claude Code native delivery is verified by a Claude CLI
  probe, and repository-scope registration depends on user trust.

## Impact

- `pkg/harness-compilation/pluginpolicy`: the Claude catalog native status and
  the probe identifier.
- `tools/harness-compat`: the defined native plugin probes.
- `e2e`: the Claude native plugin probe and tests whose expectations assumed
  Claude native delivery was unverified.
- `cmd/agent-marketplace`: plugin command tests that assumed Claude decomposes
  under `auto`.
- `docs/harness-validation/claude.md`, `site/docs/reference/plugin-support.md`.
- The registration entries, the Claude catalog shape, Factory Droid and Codex
  delivery, and the delivery-resolution rules are unchanged.
