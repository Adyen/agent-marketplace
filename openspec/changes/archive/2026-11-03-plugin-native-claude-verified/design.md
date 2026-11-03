## Context

`plugin-native-claude` writes owned `extraKnownMarketplaces` and
`enabledPlugins` entries to `~/.claude/settings.json` (user scope) and
`.claude/settings.local.json` (repository scope) over an owned marketplace
catalog. Five CI experiments with Claude Code 2.1.316 (runs 41535700517,
41541315625, 41546595871, 41600661909, 41607195657) established:

- User scope: in a trusted isolated home, a headless stream-json session sees
  Claude register the marketplace and write its own installed-plugin record
  within seconds; `claude plugin list --json` then reports the plugin enabled
  and initialization lists its namespaced skill.
- Repository scope: Claude registers the marketplace but writes no install
  record, lists no plugin, and discovers no skill, with the registration in
  `.claude/settings.local.json` or `.claude/settings.json`, with trust keyed by
  the literal and canonical workspace path, in headless and interactive
  sessions. The registration shape matches the working user-scope one.

## Goals / Non-Goals

**Goals:** verify Claude native delivery at user scope; keep repository scope
decomposed and honest; per-scope catalog status.

**Non-Goals:** changing the registration entries; writing Claude trust,
records, cache, or managed policy from Agent Marketplace; seed directories;
delivery through `.claude/skills`; Claude Cowork or Desktop; Factory Droid
and Codex delivery.

## Decisions

### D1: Trust is a probe precondition, never sync output

The probe asserts that sync wrote no Claude-owned plugin state or trust
state, then marks the probe workspace trusted in its isolated
`~/.claude.json`, standing in for the user accepting Claude's trust dialog.

### D2: Claude installs the plugin in a live headless session

The probe keeps a headless stream-json session open until Claude's own
installed-plugin record names the plugin, bounded by a deadline, without a
prompt or model response. `claude plugin list --json` and a later
initialization provide the verdict.

### D3: Per-scope native status

A catalog's `delivery.native` keeps its single-status form, which applies to
both scopes, and may instead declare `user` and `repo` entries, each with its
own status and, when verified, probe. Validation applies the probe rule to
each verified scope. Delivery resolution asks for the status at the request's
scope. `catalogs/claude.yaml` declares user `verified` with
`claude-native-plugin` and repo `unverified`.

### D4: Repository-scope negative witness

At repository scope the probe syncs a `delivery: auto` plugin and asserts
decomposed members and no native registration, so a future flip of the
repository status needs new evidence.

## Risks / Trade-offs

- The repository-scope gap is unexplained; the remaining hypothesis is that
  Claude installs project-declared plugins only after an interactive
  confirmation. It is tracked in a GitHub issue.
- Claude's background install timing varies; the probe uses a bounded
  deadline and records elapsed time.
