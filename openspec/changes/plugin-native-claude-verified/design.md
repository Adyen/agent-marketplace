## Context

`plugin-native-claude` writes owned `extraKnownMarketplaces` and
`enabledPlugins` entries to `~/.claude/settings.json` (user scope) and
`.claude/settings.local.json` (repository scope) over an owned
marketplace catalog with relative `./plugins/<name>` sources. Its probe ran
Claude Code 2.1.311 in print and stream-json sessions and never saw the
plugin load, so the catalog stayed `unverified` (its design D4).

Claude's plugin loading reference says plugins load at session start from
Claude's own records and cache; after the session starts, Claude clones or
registers each marketplace that settings declare but its records lack, then
reloads plugins. Repository-scope `extraKnownMarketplaces` apply only in a
trusted folder; a non-interactive run honors them only when trust was
already accepted interactively or `hasTrustDialogAccepted` is set for the
folder in `~/.claude.json`. The earlier probe never established trust and
ended sessions before background registration could complete.

## Goals / Non-Goals

**Goals:** a probe that witnesses the shipped registration loading at both
scopes the way a user's Claude does; the catalog flip to `verified`.

**Non-Goals:** changing the registration entries or catalog shape; writing
Claude trust, records, cache, or managed policy from Agent Marketplace; seed
directories; Claude Cowork or Desktop; Factory Droid and Codex delivery.

## Decisions

### D1: Trust is a probe precondition, never sync output

The probe asserts that sync wrote no Claude-owned plugin state or trust
state, then marks the probe workspace trusted in its isolated
`~/.claude.json`, standing in for the user accepting Claude's workspace trust
dialog. Agent Marketplace output is unchanged.

### D2: Claude registers the marketplace in a live session

The probe starts a Claude session and keeps it alive until Claude's own
known-marketplace records name the Agent Marketplace catalog, bounded by a
deadline. It does not request a prompt or a model response. A separate
`claude plugin list --json` and Claude initialization then provide the
verdict. The session mode (interactive terminal or headless) is chosen by
what CI shows Claude needs; the probe records the commands and output it ran.

### D3: Catalog flip

`catalogs/claude.yaml` declares `native: {status: verified, probe:
claude-native-plugin}` with dated evidence references to Claude's
marketplace, loading, and settings documentation. The probe identifier is
defined in the harness validation suite.

## Risks / Trade-offs

- Claude's background registration timing varies; the probe uses a bounded
  deadline and records elapsed time.
- Users who never trusted a repository see no repository-scope plugin; this
  is Claude's documented behavior and is stated in the documentation.
