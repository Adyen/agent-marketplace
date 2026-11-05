# Migrating the five marker-merge targets to proven ownership

## Promotion boundary

This plan is promoted. It supersedes nothing; it extends
[`2026-08-03-devin-first-class-harness.md`](./2026-08-03-devin-first-class-harness.md)
("the Devin plan") to the five targets that plan deliberately left alone. Every
ownership rule quoted here is quoted from the Devin plan or from
`internal/ownership/doc.go`. **This plan invents no second set of ownership
semantics.** Where a question is genuinely new — legacy markers on disk, a
downgrade path, Factory's rule-group hooks — the answer is derived from an
existing rule and the derivation is written down.

## Objective

Claude, Factory, Cursor, Windsurf, and Gemini currently decide whether they may
DELETE an entry from a user's aggregate file by looking for a `_managed_by`
string inside that same file
(`artifact.MergeWithExistingJSON`, `internal/materialize.stripManagedEntriesFromFile`).
Any tool, any user, and any copy-paste can add or remove that string, and
neither failure mode is detectable from inside the file:

- a user who hand-writes `"_managed_by": "agent-marketplace"` loses their entry
  on the next prune;
- an entry we really did generate, whose marker the user stripped, is never
  cleaned up.

The asymmetry runs in the dangerous direction: the marker path **deletes on weak
evidence**, while `internal/ownership` **fails closed and preserves**. Devin
already does the right thing (`internal/materialize/devin.go`). The objective is
to make the other five look like Devin, without a flag day and without deleting
anything a user did not explicitly authorize.

## Non-goals

- Changing what any target compiles to. No artifact path, no JSON shape, no
  diagnostic code for a *compile* decision moves.
- Migrating the Factory command-file frontmatter marker
  (`injectManagedByFrontmatter` / `isManagedCommandFile`,
  `internal/materialize/materialize.go:3640`). That is a per-file copy marker on
  a file we exclusively own, not an aggregate-entry ownership claim, and nothing
  in this plan reads or writes it.
- Migrating `.claude-plugin/plugin.json` to ownership records. See
  [Scoped out](#7-scoped-out-and-why).
- A second durable store, a second journal, or a second backup scheme. Every
  migrated target uses `internal/ownership` exactly as `internal/materialize/devin.go`
  uses it: `Open` → `Recover` (once, before any mutation) → `Proof` → `Classify`
  → patch with `pkg/harness-compilation/jsonc` → `Apply`.

---

## 1. What is actually different per target

The five are **not** uniform. `TargetInfo.AggregateFiles` is not a reliable
inventory of what is marker-merged today (see
[Bugs found while reading](#bugs-found-while-reading-present-today-not-caused-by-this-migration)); the authoritative
inventory is "every compiled artifact with `MergeWithExisting == true`", which
is set in each target's translator whenever `opts.ManagedBy()` is non-empty.

### Marker-merged files today

Paths are relative to the target's **merge output root**, which is the install
root for every target except Windsurf at repo scope, where it is
`<repo>/.windsurf` (`internal/materialize/materialize.go:1517`, `:591`).

| Target | File (repo scope) | File (machine scope) | Container shape | Merge routine | Entry identity **today** | Entry identity **after migration** |
|---|---|---|---|---|---|---|
| Claude | `.mcp.json` | `~/.claude/.mcp.json` | `{"mcpServers": {key: obj}}` | keyed-object branch of `MergeWithExistingJSON` | object key + marker | `ownership.KindMcpServer`, `McpIdentityDigest(key)` — identical to Devin's `mcp_config.json` |
| Claude | `hooks/hooks.json` | `~/.claude/hooks/hooks.json` | `{"hooks": {Event: [rule]}}` | `mergeMapOfObjectArrays` | **none** — "carries the marker" is the whole test | `ownership.KindHook`, `HookIdentityDigest(event, valueDigest)` — total identity, identical to Devin's `config.json#hooks` |
| Claude | `.claude-plugin/plugin.json` | same | `{"skills": [{name,…}], "agents": [{name,…}]}` | `mergeArrayContainer` (keyed by `"name"`) | `name` + marker | **not migrated** — see [Scoped out](#7-scoped-out-and-why) |
| Factory | `mcp.json` | `~/.factory/mcp.json` | `{"mcpServers": {key: obj}}` | keyed-object branch | object key + marker | `KindMcpServer`, `McpIdentityDigest(key)` |
| Factory | `hooks/hooks.json` | `~/.factory/hooks/hooks.json` | `{Event: [ {matcher, hooks:[…]} ]}` — event names are **top-level** keys, no `hooks` envelope, and the array elements are *rule groups*, not rules | `mergeArrayContainer` via `asObjectArray` (see bug (c)) | **none, and worse than none**: rule groups have no `name`, so every managed group is dropped and re-appended on every sync | `KindHook` with a new **rule-group tuple** (event, matcher, canonical `hooks` array) — the only target needing a new identity function |
| Cursor | `mcp.json` | `~/.cursor/mcp.json` | `{"mcpServers": {key: obj}}` | keyed-object branch | object key + marker | `KindMcpServer`, `McpIdentityDigest(key)` |
| Cursor | `hooks/hooks.json` | `~/.cursor/hooks/hooks.json` | `{"hooks": {Event: [entry]}}` | `mergeMapOfObjectArrays` | **none** | `KindHook`, `HookIdentityDigest(event, valueDigest)` |
| Windsurf | `.windsurf/mcp_config.json` | `~/.codeium/windsurf/mcp_config.json` | `{"mcpServers": {key: obj}}` | keyed-object branch | object key + marker | `KindMcpServer`, `McpIdentityDigest(key)` |
| Windsurf | — | — | Windsurf hooks are **per-event regular files** (`.windsurf/hooks/<event>.json`, `windsurf/translator.go:267`), never merge artifacts | n/a | n/a | unchanged — nothing to migrate |
| Gemini | `settings.json` (compiled as `.gemini/settings.json`, then `ArtifactRootPrefix` strips `.gemini`) | `~/.gemini/settings.json` | `{"hooks": {Event: [rule]}}` | `mergeMapOfObjectArrays` | **none** | `KindHook`, `HookIdentityDigest(event, valueDigest)` |
| Gemini | `gemini-extension.json` | `~/.gemini/gemini-extension.json` | `{"mcpServers": {key: obj}}` | keyed-object branch | object key + marker | `KindMcpServer`, `McpIdentityDigest(key)` |

### What that table means for the work

Three observations do most of the scheduling:

1. **Every MCP aggregate in the repo has the same shape.** `{"mcpServers": {key:
   object}}`, identity = the object key. That is byte-for-byte the container
   Devin's `mcp_config.json` reconciler already handles
   (`devinFragmentEntries`, `devinMcpServersKey`). Migrating MCP for all five is
   one generalized code path plus five metadata rows.
2. **Three of the four hook aggregates have the same shape as Devin's user
   hooks.** Claude, Cursor, and Gemini all wrap events in a `hooks` member whose
   values are arrays of rule objects, which is exactly
   `harness.DevinUserConfigFile`'s shape and exactly what `devinHookEntries` +
   `devinHookIdentity` already parse. Factory is the odd one out: no envelope,
   and the array elements are *rule groups* (`{matcher, hooks: [...]}`), so its
   identity tuple has to be defined from scratch.
3. **Claude has a fourth file nothing else has.** `.claude-plugin/plugin.json`
   is a name-keyed **array** manifest, which `internal/ownership` does not model
   (`EntryKind` is exactly `mcp-server` and `hook`). It is the only container
   that would force a new `EntryKind`, and it is the least dangerous file in the
   set. It is scoped out.

### Scope multiplicity

Devin has two scopes and three files. The five each write their aggregates at
**both** repo and machine scope, and the ownership store is keyed by canonical
absolute root, so each (target, scope) pair gets its own record — nine
(target, root) stores in the worst case for one repo. Nothing in
`ownership.Open` needs to change; the store is already root-generic. The only
per-target decision is *which directory is the root*, and there is exactly one
non-obvious answer:

- **Windsurf repo scope must open its store at `<repo>/.windsurf`, not at
  `<repo>`.** `targetmeta` declares Windsurf's `RepoRoot` as `"."`, so the plan
  path's `outDir` is the repository root itself. Rooting ownership state at a
  whole repository would make the record key mean "this repo", not "this
  harness root", and would collide with any future repo-root target. Materialize
  already computes `<repo>/.windsurf` as the merge/prune root
  (`materialize.go:591`, `:1517`); the store must use the same directory.

### This migration is also the repo-scope safety-backup answer

`internal/backup` is machine scope only: `pipeline.CollectManagedPaths` filters
on `config.Source.IsMachineScope`, so `<repo>/.cursor/mcp.json`,
`<repo>/.claude/hooks/hooks.json`, `<repo>/.gemini/settings.json` and
`<repo>/.windsurf/mcp_config.json` are merged into with no archive at all. Once
M0 (bead `.27`) made `AggregateFiles.Project` truthful, adding a repo-scope
collector became a two-line edit, and bead `.30` evaluated it and **decided not
to**. The reasoning is recorded in full in the `internal/backup` package doc;
the two load-bearing points for this plan are:

- `internal/backup`'s archive namespace is `<target>/<timestamp>.tar.gz`, with no
  root dimension, and `Restore` takes only a target. Covering repo scope there
  correctly means re-keying the layout, `Restore`, `ListBackups` and the
  `backup restore` CLI by canonical root — the root keying `internal/ownership`
  already has and that this plan already relies on.
- Each migration bead below therefore delivers repo-scope backup coverage for
  its target as a side effect, at both scopes, mandatory, with raw pre-mutation
  bytes: M4 for Cursor, M5–M8 for the rest. No separate repo-scope backup bead
  is needed or wanted.

The interim exposure is bounded by the write path itself: after beads `.26` and
`.28` an unmanaged entry keeps its exact bytes, its position and the comments
above it, so a merge does not damage handwritten content and there is nothing
for a repo-scope archive to recover.
`materialize.TestRepoScopeAggregateMergePreservesHandwrittenContent` pins that
property over every declared repo-scope aggregate of every not-yet-migrated
target, and must keep passing until the last one flips.

One consequence for M9: `.claude-plugin/plugin.json` is scoped out of ownership,
so it is the one aggregate that will still have no repo-scope backup after this
plan completes. That is acceptable for the reasons M9 already gives — it is a
manifest we author, holding inert metadata, and M9 makes it never delete — but
it should be stated here rather than discovered later.

Note also that "git is the implicit recovery mechanism for anything in a repo"
is not a claim this product can make. It writes exactly one `.gitignore` entry
in its entire lifetime — `.agents/config.local.yaml`, at the repository root
(`pipeline.EnsureLocalConfigGitignored`) — and writes no `.gitignore` into any
generated harness directory. Whether `.claude/`, `.cursor/`, `.gemini/` or
`.devin/` is ignored is the user's own repository policy.

---

## 2. The adoption story for existing installs

Every current user has files full of `_managed_by` markers and no ownership
records. This section is the whole point of the bead, so it is written as a
state machine rather than a narrative.

### The transition primitive: legacy-marker normalization

Adoption is byte-neutral: `ownership.ExactAdoptionCandidates` only nominates an
existing entry whose **identity digest and value digest already equal the
desired entry's**. A marked entry on disk fails both tests, because the
`_managed_by` member is part of the object:

- for MCP, the value digest differs (the object has one extra member);
- for hooks, the **value** digest also differs (the `_managed_by` member is
  part of the object), and the **identity** digest is the event plus the value
  digest, so the extra member changes both.

So the first thing every migrated target needs is a single normalization rule,
implemented once, in one place:

> **Legacy-marker normalization.** When reading an EXISTING aggregate entry, the
> member `_managed_by` whose value is exactly `agent-marketplace` is removed
> before the entry is canonicalized for identity or value digesting. It is never
> removed from the file, and it is never consulted as evidence of ownership by
> the sync path.

With that rule, a marked entry we really did generate digests **identically** to
the marker-free entry we now desire, so `ExactAdoptionCandidates` nominates it
and adoption records the correct digests **without writing a byte**. That is the
"byte-neutral adoption" the acceptance criteria require, and it is a read-side
normalization, not an ownership inference: a hand-written marked entry that does
not exactly match a desired entry is not nominated, and a hand-written marked
entry that *does* exactly match a desired entry is adopted — which is the same
outcome `doctor --repair` already produces for Devin, under the same
user-authorized command.

Normalization is deliberately narrow: exact member name, exact string value,
existing entries only, and it is deleted with the marker in release N+2.

### Is adoption automatic?

**No. Adoption requires `doctor --repair`, exactly as for Devin.** The Devin
plan's rule is "Without proof of ownership, sync preserves existing entries and
refuses collisions or pruning", and its adoption surface is `doctor --repair`
(`--yes` for the non-interactive batch). Auto-adopting during sync would mean
sync silently converting "an entry that happens to look like ours" into "an
entry we are licensed to delete", which is precisely the inference this
migration exists to remove. There is no second set of semantics here.

### First sync after upgrade, per entry

Assume the target has been migrated and the user has not run `doctor --repair`.
`ownership.Open` finds no record, so `Proof` is the zero value and
`Proof.Classify` returns `StatusUnmanaged` for **everything**.

| Entry on disk | Desired this run | Outcome | Bytes changed? | Can the user lose data? |
|---|---|---|---|---|
| marked, exactly equals desired (after normalization) | yes | preserved as-is; no patch; reported as an adoptable entry with a `run doctor --repair` hint | no | no |
| marked, differs from desired | yes | **collision**: the desired entry is not written, the existing entry is preserved, one diagnostic per entry | no | no |
| marked, no longer desired | — | preserved (stale leftover); reported as an adoptable-for-removal candidate | no | no |
| unmarked user entry, key collides with desired | yes | collision, preserved — unchanged from today's behaviour | no | no |
| absent | yes | written; recorded as owned in the same journaled generation | yes | no |
| hand-written marker, never generated by us | either | preserved forever; **can only ever be deleted through an interactive per-entry confirmation in `doctor --repair`** | no | **no — this is the bead's headline criterion** |

Note the third row. Today, a marked-but-no-longer-desired entry is deleted on
sync. After migration it is preserved. That is a deliberate, permanent change of
behaviour in the safe direction, and it is the cost of the migration: **stale
generated entries accumulate until the user runs `doctor --repair`.**

### The collision severity problem, and the marker's remaining role

Row 2 is the one that would ruin the upgrade if handled naively. For Devin, an
unmanaged collision is an error diagnostic and `sync` exits non-zero. If the
five inherited that unchanged, *every* existing user's first sync after upgrade
would fail, for every MCP server and hook they have installed, until they ran a
command they have never heard of.

**Decision.** For the transition release only, a collision or a
would-have-pruned entry that carries a legacy marker is reported at **warning**
severity with a stable code and an explicit `doctor --repair` hint; the same
situation without a legacy marker keeps error severity. `sync` therefore still
exits zero for an un-repaired legacy install and non-zero for a genuine
unmanaged-user-entry collision. In release N+2, when the marker is dropped, the
softened branch is deleted and every collision is an error again.

That gives the marker its **entire remaining role**, which the bead requires to
be documented:

1. **Transition-window severity input** (release N+1 only): a legacy marker
   downgrades a collision/no-prune report from error to warning and adds the
   repair hint. It licenses no mutation whatsoever.
2. **Admissible orphan evidence** (release N+1 only): a legacy marker on an
   entry that is not desired is admissible as `ownership.EvidenceLegacyMarker`
   for an **orphan nomination** in `doctor --repair`. An orphan nomination is
   not a deletion: `ownership.Apply` refuses it unless `Mutation.Confirmed` is
   set, `--yes` never sets it, and a fresh safety backup is taken first. So a
   hand-written marker can, at absolute worst, cause the user to be *asked*
   about their own entry on a TTY.
3. Nothing else. It is never written, never read by the sync mutation path,
   never an ownership answer.

Point 2 does widen `internal/ownership`'s evidence rule, which today says
evidence "is only ever produced from a retained safety-backup manifest or
journal". The widening is explicit, is a new named `EvidenceKind` so it is
visible in the preview output ("legacy `_managed_by` marker"), is gated behind
the same interactive-confirmation-plus-backup wall as every other orphan, and is
deleted in N+2. The alternative — never nominating pre-migration leftovers —
means every user carries their pre-upgrade stale entries forever with no
supported way to remove them, which is worse.

### The mixed window

Between the first migrated sync and a completed repair, a single file will
contain some proven entries (written after the upgrade) and some marked-only
entries (written before it). This is per-entry and needs no special handling:

- proven entries get the full Devin lifecycle — drift is reported and replaced,
  stale ones are pruned, values are updated;
- marked-only entries are unmanaged — preserved, never pruned, collisions
  refused.

**No state in this table can delete a user's entry without an interactive
per-entry confirmation.** The set of states that can delete anything at all is
exactly one: a proven owned entry that has become stale, deleted by sync — and
"proven" can only be reached through a journaled `Apply` we performed or an
adoption the user ran.

### A user who never runs `doctor --repair`

They keep working. Their existing entries are never touched. New resources are
written and proven normally. Removed resources leave stale entries behind, each
of which produces one warning per sync naming the file and the non-secret entry
identity plus the repair hint. Changed resources produce a collision warning
instead of an update. Nothing is lost; the install slowly drifts. `status` and
`sync --check` must surface the count so this is visible rather than nagging
noise buried in a diagnostic list.

---

## 3. Does the marker keep being WRITTEN?

**Decision: no. A migrated container stops writing `_managed_by` the moment it
is migrated.** The marker becomes read-only legacy evidence for exactly one
release (N+1) and every read of it is deleted in N+2. Mechanically this is
`compile.Options.ManagedBy()` returning empty for the migrated artifact, which
also clears `MergeWithExisting` and keeps the fragment out of the generic plan
path — the same routing Devin gets from `splitDevinAggregateArtifacts`.

Reasoning, and what breaks either way:

**If we kept writing it (rejected).** There would be two sources of truth for
every entry, and they would disagree the first time a user copied a proven entry
into a different root: the copy carries a marker, no record exists for it, and
the legacy path in that root would delete it. Worse, the marker would have to be
part of the recorded value digest forever, so "byte-neutral adoption" and
"marker-free desired output" would become permanently incompatible, and the
migration could never finish. The only thing this buys is compatibility with an
older binary, which is addressed below more cheaply.

**If we stop writing it (chosen), here is exactly what breaks.**

- **An older binary run against a migrated file.** Its `isManagedEntry` returns
  false for our new, markerless entries, so:
  - for keyed-object containers (every MCP file) the old binary *preserves* the
    entry and declines to overwrite it — the entry simply stops being updated by
    the old binary. Safe.
  - for array containers (Claude/Cursor/Gemini hooks, Factory rule groups) the
    old binary appends its own marked copy next to ours, producing **duplicate
    hooks**. This is the real breakage. It is a downgrade-only failure, it is
    visible, and it is recoverable (see [Rollback](#5-rollback)). It must be in
    the changelog and in the release notes; it is not preventable while the two
    binaries disagree about what "ours" means.
- **Other tooling that reads `_managed_by`.** A repo-wide search finds no reader
  outside `internal/materialize`, `pkg/harness-compilation/artifact`, and their
  tests. The only other user-visible use of the string is the Factory
  command-file *frontmatter* marker, which is a different mechanism on files we
  own outright and is explicitly out of scope. Third-party tooling that keys off
  the marker is undocumented and unsupported; the changelog should say so
  plainly rather than promising a contract we never made.
- **User expectations.** Some users grep for the marker to tell "mine" from
  "theirs". `doctor` (not just `--repair`) must gain a listing of proven owned
  entries per aggregate file so that question still has an answer. This is a
  real UX regression if it is not built.

---

## 4. Ordering and dependencies

Four groundwork beads must land before any target moves. After that, one pilot
target proves the shape end-to-end, three targets can go in parallel, and
Factory goes last because it is the only one that needs a new identity function.

```
M1 ─┐
M2 ─┼─→ M4 (Cursor, pilot) ─┬─→ M5 (Claude)   ─┐
M3 ─┤                       ├─→ M6 (Gemini)   ─┼─→ M10 (drop the marker, release N+2)
M0 ─┘                       ├─→ M7 (Windsurf) ─┤
                            └─→ M8 (Factory)  ─┘
M9 (plugin.json no-delete)  — independent, needs only M0
```

- **M0–M3 are strictly sequential-ish groundwork** (M0 and M2 are independent of
  each other and can be done in parallel; M1 depends on M0; M3 depends on M1
  and M2).
- **M4 is a gate.** No other target bead starts until Cursor is merged, because
  M4 is where the generalized engine, the transition diagnostics, and the
  adoption flow meet reality for the first time. Anything the pilot teaches gets
  folded back into M1–M3 before it is replicated four more times.
- **M5, M6, M7 are parallel** after M4. They touch disjoint translators and
  disjoint metadata rows; the only shared file is the target-metadata table,
  which is a one-line-per-target edit.
- **M8 (Factory) is last of the five** and is the only one that also carries a
  bug fix (see bug (c)).
- **M9 is independent** and can be picked up at any point after M0 by anyone.
- **M10 cannot start until all of M4–M9 have shipped in a release** and the
  transition window has elapsed. It is a separate release, not a separate
  commit.

---

## 5. Rollback

Ownership state lives in `$XDG_STATE_HOME/agent-marketplace`, outside the repo
and outside the cache. `git checkout` does not undo it, `rm -rf ~/.cache/...`
does not undo it (by design: "Cache deletion has no ownership effect"), and
reverting the binary does not undo it. There are therefore exactly three
rollback levers, and the plan requires all three to be documented in
`site/docs/how-to/adopt-agent-marketplace-in-a-repository.md` **as part of the pilot bead M4**, not afterwards.

1. **Restore the file.** Every mutated aggregate has raw pre-mutation bytes
   under the state root, mandatory even when configured backups are disabled,
   five newest retained per canonical file. `doctor --repair` must print the
   backup directory for each file it touched so the path is discoverable without
   reading this document. Restoring a backup is the only lever that recovers
   *content*.
2. **Forget ownership.** Deleting the per-root record directory
   (`Store.StateFile()`'s parent, printed by `doctor`) makes every entry in
   every aggregate under that root unmanaged. That is the fail-closed direction
   and it is safe by construction: an unmanaged entry is preserved, never
   pruned, and collisions are refused. It cannot lose data; it can only leave
   stale entries. This is the lever for "the new path is deleting/refusing
   things I do not understand".
3. **Revert one target.** Each of M4–M8 migrates exactly one target, so a bad
   migration is one revert. A reverted binary re-enables marker-merge for that
   target — and because migrated entries have no marker, the old path treats
   them as user entries: MCP entries are preserved and go stale, hook entries
   get duplicated on the next sync (section 3). The documented recovery for the
   duplication is lever 1.

**Not proposed: a runtime kill switch.** An `AGENT_MARKETPLACE_LEGACY_MERGE=<targets>`
env var was considered and rejected. It would have to keep both code paths alive
and mutually consistent for the whole transition, doubling the crash-injection
matrix, and it would let a user toggle back into a mode that deletes on weak
evidence — reintroducing the exact hazard, on a flag, with no audit trail. Lever
2 covers the same incident ("make it stop doing things") without that cost. If
the pilot shows lever 2 is insufficient, revisit this decision in M4's review,
not silently in code.

---

## 6. How each step is proven

The existing bar is set by `internal/ownership/journal_crash_test.go`,
`internal/materialize/devin_test.go`, and the Devin plan's verification policy.
The migration does not lower it. Every target bead M4–M8 must add, **for each
aggregate file it migrates**:

- **Crash injection at every boundary.** One test per `ownership.CrashPoint`
  (`before-backup`, `after-backup`, `after-journal-prepared`,
  `after-staged-write`, `after-aggregate-rename`, `after-journal-renamed`,
  `after-state-commit`, `after-journal-cleanup`) driven through the target's own
  reconciler, asserting that a subsequent `Recover()` leaves the file and the
  record in exactly one of the two legal states and that no user entry is lost
  in either.
- **Byte-identical destination on refusal.** For every refusal branch —
  malformed JSON, duplicate keys, unmanaged collision, shape mismatch, missing
  or corrupt state — assert the destination file's bytes are unchanged with
  `bytes.Equal` against the pre-run read, not against a golden.
- **No staging residue.** After every refusal and after every injected crash,
  assert the output root and the state root contain no `.tmp`/staging entries,
  by directory listing rather than by known filename.
- **Byte-neutral adoption.** Read the file, run `doctor --repair --yes`, read it
  again, assert byte equality; separately assert the record now proves the
  entries. This is the acceptance criterion "existing markers are adopted
  byte-neutrally" and it must be a direct assertion, not an inference from a
  golden.
- **Hand-written marker cannot delete.** Seed an aggregate with an entry that is
  not desired and carries a hand-written `_managed_by`, run `sync` (repeatedly,
  and with the resource removed from config), assert the entry survives byte
  for byte and that the only path that removes it is an
  `ownership.Mutation{Confirmed: true}`. One such test per migrated file. This
  is the bead's headline criterion and it gets its own named test per target.
- **Non-vacuity.** Every preservation test must be paired with a mutation test
  proving the same fixture *can* be changed by the code path under test, so a
  future refactor that makes the reconciler a no-op fails rather than passes.
  Similarly, every crash test asserts the crash actually fired
  (`errors.Is(err, ownership.ErrInjectedCrash)`).
- **Preservation of user syntax.** Comments, key order, and hook order in the
  destination survive reconciliation; handwritten hooks precede generated ones;
  duplicate desired identities collapse before patching. These are Devin plan
  rules and they become true for the five for the first time (see bug (h)).
- **Per-file atomicity.** A malformed file blocks only that file: with two
  aggregates in one root and one of them corrupt, the other still reconciles and
  the corrupt one is backed up, unchanged, and reported with a parse location.
- **Golden fixtures for the transition window**: a fixture install with
  pre-migration marked entries, asserting the exact diagnostic codes and
  severities of section 2's table, including that `sync` exits zero.
- **E2E**: one declarative `TestCase` in `e2e/table_test.go` per migrated
  target, covering install → sync → remove → sync with a handwritten neighbour
  entry present throughout.
- **Benchmarks**: `sync` and the target's reconcile path are user-visible hot
  paths. Capture `before.txt`/`after.txt` with `benchstat` per the repository
  policy; the added per-entry digesting is the thing being measured.

M1–M3 additionally need: table-driven shape tests for the generalized engine
against all four container shapes; a test that legacy-marker normalization is
exact-member/exact-value and does not strip a user's `_managed_by: something-else`;
and a test that `EvidenceLegacyMarker` can never reach `Apply` without
`Confirmed`.

---

## 7. Scoped out, and why

Uniformity is not a reason to migrate. Three things are deliberately left alone.

### `.claude-plugin/plugin.json` — scoped out of ownership, moved to no-delete (M9)

Migrating it would force a third `ownership.EntryKind` for name-keyed array
manifests, a third identity function, and a third set of crash tests, for the
file in the set with the lowest blast radius: it is a manifest we author, its
entries are inert metadata rather than executable hooks or credential-bearing
MCP servers, and Factory reached the conclusion years-equivalent ago that the
plugin manifest "is a simple identity file … owned by the harness"
(`factory/translator.go:121`) and stopped generating entries into it entirely.
Paying the full ownership cost here is a poor risk-to-benefit trade.

But leaving it exactly as-is would violate the bead's criterion that a
hand-written marker can no longer cause a deletion. So M9 takes the cheap half:
**stop deleting from `plugin.json`.** Sync adds and updates entries it can
recognize and never removes one; a stale entry is reported with a hint. The
tradeoff is honest and small: `plugin.json` accumulates entries for resources
the user removed, which is metadata clutter in a file whose entries are inert.
If that clutter turns out to matter, the follow-up is to stop generating
`plugin.json` entries at all, matching Factory — not to reintroduce
marker-based deletion.

### Factory `settings.json` and Gemini `mcp.json` — nothing to migrate

Both are declared in `TargetInfo.AggregateFiles` and neither is emitted by any
compiler (see bug (a)). They are metadata errors, fixed in M0 as a one-line
deletion each. Writing migration code for a file that does not exist would be
pure ceremony.

### Windsurf hooks — nothing to migrate

Windsurf compiles hooks to per-event regular files under `.windsurf/hooks/`,
not to a merge artifact. They are already handled by the normal
prune-what-we-wrote path. Windsurf's migration is one file,
`mcp_config.json`, in two scopes.

---

## Bead breakdown

Each scope statement below is written to be pasted into a bead description.

### M1. Extract a target-agnostic ownership-backed aggregate reconciler

`internal/materialize/devin.go` contains a complete, tested implementation of
ownership-backed aggregate reconciliation that is hard-wired to Devin: it
returns early unless `key.target == hcc.TargetDevin`, it switches on Devin's
three relative paths to pick a fragment shape, and its container navigation
knows Devin's envelopes. Extract the reusable core — desired-entry collection,
`Open`/`Recover`/`Proof`/`Classify`, the jsonc patch plan, `Apply`, per-file
diagnostics and reporting — into a reconciler parameterized by a small
per-target descriptor: the aggregate's relative path, its container shape
(keyed object of objects, or map of event → array of objects), the container
member to navigate to (`""`, `mcpServers`, `hooks`), and the identity function
for its entries. Devin becomes the first caller of the generalized engine and
must be **behaviourally identical** afterwards: the existing Devin test suite,
including every crash-injection test, passes unchanged with no edits to
assertions. No other target is wired up in this bead.

*Acceptance:* Devin's reconciliation behaviour, diagnostics, codes, ordering,
and byte output are unchanged; `devin_test.go` and `journal_crash_test.go` pass
without modification; the generalized engine has table-driven shape tests for
all four container shapes in the repo (keyed object, `hooks`-enveloped event
arrays, bare event arrays, and Devin's bare `hooks.v1.json`); no target other
than Devin routes through it yet.

### M2. Legacy-marker normalization and legacy-marker orphan evidence

Add two narrow, expiring facilities to support the transition. First, a
normalization applied when reading an EXISTING aggregate entry: remove the
member named exactly `_managed_by` whose value is exactly the string
`agent-marketplace` before canonicalizing the entry for identity or value
digesting. It is never removed from the file and is never treated as evidence of
ownership by any sync path. This is what makes `ExactAdoptionCandidates`
nominate a pre-migration marked entry, and therefore what makes adoption
byte-neutral. Second, add `ownership.EvidenceLegacyMarker` as a new
`EvidenceKind` admissible only for orphan NOMINATION, subject to the existing
`Mutation.Confirmed` wall and fresh-backup requirement, and rendered in preview
output as "legacy `_managed_by` marker". Both facilities are marked for deletion
in M10 with a comment naming that bead.

*Acceptance:* normalization strips only the exact member/value pair and leaves
`_managed_by: <anything-else>` untouched, proven by test; a marked entry and its
markerless desired counterpart produce identical identity AND value digests for
both `KindMcpServer` and `KindHook`; `EvidenceLegacyMarker` cannot reach
`Apply` without `Confirmed`, proven by test; `--yes` never confirms an orphan
regardless of evidence kind; no sync code path can obtain a `Proof` or a
`Status` from the marker.

### M3. Transition-window diagnostics and multi-target repair surface

Two user-facing surfaces currently assume Devin. Generalize both. In
materialization, add the transition severity rule: a collision or a
would-have-been-pruned entry that carries a legacy marker is reported at warning
severity with a new stable diagnostic code and a `doctor --repair` hint, while
the same situation without a legacy marker keeps error severity — so an
un-repaired legacy install exits zero and a genuine unmanaged-user collision
still exits non-zero. In `cmd/agent-marketplace/doctor.go` and
`internal/materialize/devin_repair.go`, replace the hard-coded Devin target and
root lookup with an iteration over every target that declares ownership-backed
aggregates, preserving the repair contract exactly: preview is read-only, off-TTY
is preview-only, `--yes` adopts exact matches and never removes an orphan, exit
0 = clean/repaired/declined, exit 1 = corrupt state or partial failure. Add to
plain `doctor` a per-file listing of proven owned entries, which is the
replacement for users who currently grep for `_managed_by`. Register the new
diagnostic codes in the catalog.

*Acceptance:* the repair contract's existing tests pass with Devin as one of N
targets rather than the only one; a fixture with two migrated targets in one
repo previews and repairs both, independently, with per-file failure isolation;
the severity rule is proven in both directions (legacy marker → warning + exit
0, no marker → error + exit 1); `doctor` lists owned entries per aggregate file;
every new code is in `diag/codes.md`.

### M0. Make target metadata tell the truth about aggregate files

`TargetInfo.AggregateFiles` is documented as the single source of truth for
"the shared aggregate configuration files the target writes", and four of six
declarations are wrong: Claude omits `hooks/hooks.json` and
`.claude-plugin/plugin.json`; Cursor omits `hooks/hooks.json`; Factory declares
`settings.json`, which no compiler emits, and omits `hooks/hooks.json`; Gemini
declares `mcp.json`, which no compiler emits, and omits `settings.json`, which
is where its hooks actually go. All five populate only the `User` scope even
though every one of these files is also written at repo scope. Because
`pipeline.CollectManagedPaths` reads this field to decide backup coverage, the
omissions are a live backup gap today. Correct every declaration for both
scopes, and replace the `SymlinkOnly` proxy in
`materialize.aggregateProtectedPaths` — which currently means "is Devin" — with
an explicit per-target ownership declaration, initially true only for Devin.
(Landed as `TargetInfo.OwnershipReconciledAggregates` in bead
agent-marketplace-5exj.29 — that is the field each migration bead below flips.)
Add a test that pins each target's declared aggregate set against the
paths its compiler really emits with `MergeWithExisting` set, in the style of
`TestDevinAggregateNamesMatchCompiledArtifactPaths`, so the two cannot drift
again.

*Acceptance:* declared aggregate files equal compiled merge-artifact paths for
all six targets in both scopes, proven by a test that fails if either side
changes; no target declares a file no compiler emits; `CollectManagedPaths`
covers every declared aggregate; `aggregateProtectedPaths` keys off
`OwnershipReconciledAggregates` and `SymlinkOnly` returns to meaning only what
its name says; Devin behaviour is unchanged.

### M4. Migrate Cursor to proven ownership (pilot)

Cursor is the pilot because it has one file of each shape and no idiosyncrasies:
`mcp.json` is a `{"mcpServers": {key: object}}` map, and `hooks/hooks.json` is a
`{"hooks": {Event: [rule]}}` map of event arrays — the same two containers Devin
already reconciles. Declare both as ownership-backed for both scopes, stop
setting `ManagedByField` on the Cursor MCP and hook artifacts so they are no
longer marker-merged and no longer reach the generic plan path, route them
through the M1 engine with `McpIdentityDigest` and `HookIdentityDigest` identities, and
protect them from `stripManagedEntriesFromFile`. Because this bead is the first
real migration, it also owns the user-facing documentation of the whole
transition: the rollback levers, the `doctor --repair` adoption step, and the
downgrade caveat, in `site/docs/how-to/adopt-agent-marketplace-in-a-repository.md`, `CHANGELOG.md`, and `AGENTS.md`.

*Acceptance:* the full test bar in section 6 for both Cursor files; a
pre-migration fixture with marked entries adopts byte-neutrally under
`doctor --repair --yes`; a hand-written `_managed_by` in `.cursor/mcp.json`
survives an unbounded number of syncs including one where the resource is
removed from config; no marker is written into either file; the transition
warnings appear and `sync` exits zero for the un-repaired fixture; rollback and
adoption are documented.

### M5. Migrate Claude MCP and hooks to proven ownership

Claude's `.mcp.json` and `hooks/hooks.json` are the same two shapes as Cursor's
and reuse the M4 wiring verbatim. Claude's third merge file,
`.claude-plugin/plugin.json`, is explicitly NOT part of this bead: it stays on
the marker path here and is handled by M9. That split is the point of this
bead's care — after it lands, one Claude root contains one file still governed
by markers and two governed by proof, and the prune walk, the protection set,
and the repair preview must all handle that mixture without touching the wrong
file.

*Acceptance:* the full test bar in section 6 for `.mcp.json` and
`hooks/hooks.json`; a test asserting `.claude-plugin/plugin.json` is still
marker-merged and is NOT in the protected set after this bead; a test asserting
the ownership-backed files are never visited by `stripManagedEntriesFromFile`;
no marker written into the two migrated files.

### M6. Migrate Gemini to proven ownership

Gemini's two merge files are misleadingly named and must be handled by their
real paths, not their declared ones: MCP servers go to `gemini-extension.json`
(a `{"mcpServers": {key: object}}` map) and hooks go to `settings.json`
(compiled as `.gemini/settings.json` and then stripped by
`ArtifactRootPrefix`, which is the only place in the repo where the declared
aggregate path and the compiled artifact path legitimately differ). Migrate both
with the M4 wiring, and add an assertion that the prefix-stripping and the
ownership path agree on the relative path, because a mismatch here would open a
store keyed on a file that does not exist while the real file stayed on the
marker path.

*Acceptance:* the full test bar in section 6 for both files; an explicit test
that the `ArtifactRootPrefix`-stripped artifact path equals the declared
ownership-backed relative path in both scopes; no marker written into either
file.

### M7. Migrate Windsurf `mcp_config.json` to proven ownership

Windsurf has exactly one merge file, `mcp_config.json`, and one non-obvious
decision: its `RepoRoot` is `"."`, so the plan path's output root at repo scope
is the repository itself, while the merge and prune root is `<repo>/.windsurf`.
The ownership store must be opened at `<repo>/.windsurf` so the record key means
"this harness root" rather than "this repository"; at machine scope the root is
`~/.codeium/windsurf` with the same relative filename. Windsurf hooks are
per-event regular files and are explicitly not part of this bead.

*Acceptance:* the full test bar in section 6 for `mcp_config.json` in both
scopes; a test pinning the repo-scope store root to `<repo>/.windsurf` and
proving no state is created keyed on the repository root; a test that the
machine-scope and repo-scope records for the same relative filename are
independent; no marker written.

### M8. Migrate Factory to proven ownership, with a rule-group hook identity

Factory's `mcp.json` is the standard keyed map. Factory's `hooks/hooks.json` is
the only container in the repo with no analogue: event names are top-level keys
and the array elements are rule GROUPS (`{matcher, hooks: [...]}`), not rules.
Today `MergeWithExistingJSON` routes it through the name-keyed array branch,
where rule groups have no `name`, so every managed group is dropped and
re-appended on every single sync — the file churns even when nothing changed and
managed groups have no stable position. Define a rule-group identity as the
canonical tuple of event, matcher, and the canonicalized `hooks` array, add it
as a target-specific rule-group identity function, and migrate both Factory files
with the M4 wiring. Fixing
the churn is part of this bead, not a follow-up: an idempotent re-sync must
leave the file byte-identical.

*Acceptance:* the full test bar in section 6 for both files; an idempotence test
asserting a second sync with unchanged config writes zero bytes to
`hooks/hooks.json`; a test that two rule groups differing only in matcher get
distinct identities and that reordering the members of a rule group's inner hook
objects does not; handwritten rule groups keep their relative order and precede
generated ones; no marker written.

### M9. Stop deleting entries from `.claude-plugin/plugin.json`

`.claude-plugin/plugin.json` is deliberately not migrated to ownership records
(a name-keyed array manifest would need a third `EntryKind` for the least
dangerous file in the set), but it must stop being able to delete a user's entry
on the strength of a string inside the file. Change its merge behaviour to
add-and-update only: an entry that carries the marker and is no longer desired
is left in place and reported with a hint, rather than removed. Nothing else
about the file changes — it keeps its marker, keeps its merge routine, and keeps
its output shape. Document the accepted consequence: `plugin.json` accumulates
entries for removed resources, which is inert metadata in a file whose entries
Claude does not execute.

*Acceptance:* no code path removes an entry from `.claude-plugin/plugin.json`,
proven by a test that removes a resource from config and asserts the entry
survives; a hand-written marked entry survives every sync; the stale-entry hint
is emitted once per stale entry with a stable code; the file's byte output for
the add/update cases is unchanged from before the bead.

### M10. Drop the marker (release N+2)

One release after the last target migration has shipped, delete the transition
machinery in a single bead so no half-state survives: legacy-marker
normalization, `EvidenceLegacyMarker` and its preview rendering, the softened
collision severity and its diagnostic code, and — for every migrated container —
the `managedByField`/`managedByValue` plumbing that no longer has a writer. What
remains of `_managed_by` afterwards is exactly two things, both out of scope:
`.claude-plugin/plugin.json` (M9) and the Factory command-file frontmatter
marker. Restore error severity for all collisions and update the catalog, the
changelog, and `AGENTS.md`. This bead must not run until telemetry or support
evidence shows the adoption path has actually been exercised in the field; if it
has not, the correct action is to extend the window, not to delete the ramp.

*Acceptance:* no marker read or write remains outside `plugin.json` and the
Factory command frontmatter; every collision is an error again and `sync` exits
non-zero for an unadopted entry; the deleted diagnostic codes are marked
retired rather than reused in `diag/codes.md`; a user who never adopted gets a
clear, actionable error naming the file, the entry identity, and the manual
remedy.

---

## Acceptance criteria for the migration as a whole

1. Each of the five targets decides aggregate-entry ownership from
   `internal/ownership` rather than an in-file marker, for every container
   listed as migrated in the [per-target table](#marker-merged-files-today).
2. Existing markers are adopted byte-neutrally through `doctor --repair`, proven
   by a read-run-read byte-equality assertion per migrated file.
3. A hand-written `_managed_by` string can no longer cause an entry to be
   deleted by any code path, in any state, without an interactive per-entry
   confirmation and a fresh safety backup — including in
   `.claude-plugin/plugin.json`, which achieves this by never deleting at all.
4. The marker's remaining role — transition-window severity input and
   admissible orphan evidence, both expiring in M10 — is documented in this
   plan, in `AGENTS.md`, and in the changelog.
5. A user who upgrades and never runs `doctor --repair` loses nothing, sees a
   bounded and actionable set of warnings, and gets exit code 0 from `sync`.
6. Ownership state, backups, journaling, drift, adoption, and orphan semantics
   are those of the Devin plan, unchanged, with no second implementation.
7. Crash injection, byte-identical-on-refusal, no-staging-residue, and
   non-vacuity coverage exists for every migrated file, at the bar Devin set.
8. `TargetInfo.AggregateFiles` matches the compiled merge-artifact paths for
   every target in both scopes, pinned by test.

---

## Bugs found while reading (present today, not caused by this migration)

These are real defects in the tree at `0df4328`. Several are fixed as a side
effect of a bead above; the rest need their own beads.

- **(a) `TargetInfo.AggregateFiles` is wrong for four of six targets.** Claude
  omits `hooks/hooks.json` and `.claude-plugin/plugin.json`; Cursor omits
  `hooks/hooks.json`; Factory declares `settings.json`, which no compiler emits,
  and omits `hooks/hooks.json`; Gemini declares `mcp.json`, which no compiler
  emits, and omits `settings.json`, which is where its hooks go. Since
  `pipeline.CollectManagedPaths` reads this field for backup coverage, those
  machine-scope files are **mutated without a backup today**, which is the same
  class of gap the field was introduced to close for Devin. Fixed by M0.
- **(b) `AggregateFiles.Project` is empty for all five**, even though every one
  of those files is written at repo scope too. The field's own doc comment calls
  it the single source of truth. Fixed by M0.
- **(c) Factory `hooks/hooks.json` is rewritten on every sync.** Its event
  arrays hit `mergeArrayContainer`, which keys entries by a `name` member that
  Factory rule groups do not have, so every managed group takes the "unnamed
  desired entry: cannot dedupe; mark and append" path while existing managed
  unnamed groups are dropped. The file therefore churns on idempotent syncs and
  managed groups have no stable order relative to user entries. Fixed by M8.
- **(d) `MergeWithExistingJSON` destroys comments and key order for all five
  targets on the WRITE path.** Bead `agent-marketplace-5exj.18` moved the
  *prune* path (`stripManagedEntriesFromFile`) onto `pkg/harness-compilation/jsonc`,
  but the merge path still ends in `json.MarshalIndent(existing, ...)`
  (`artifact/merge.go:134`), which re-serializes the entire document. Any sync
  that merges into a user's `.cursor/mcp.json` or `~/.claude/.mcp.json` today
  reorders their keys, drops their comments, and collapses duplicate keys
  last-value-wins — the exact three properties the Devin plan requires to
  survive. This is independent of ownership and is arguably more damaging than
  the marker itself; the migration fixes it incidentally for migrated files
  (the M1 engine patches through `jsonc`), but it remains live for
  `.claude-plugin/plugin.json` and for any future merge artifact. **Worth its
  own bead regardless of this migration.**
- **(e) Gemini's `gemini-extension.json` is generated with only a `mcpServers`
  member** (`gemini/translator.go:204`), with no `name` or `version`. A Gemini
  extension manifest normally requires identity fields; if that is true of the
  installed CLI, the file we write may not load at all. Unverified against a
  live Gemini CLI — worth a probe before assuming the MCP path works end to end.
- **(f) `aggregateProtectedPaths` uses `SymlinkOnly` as a proxy for
  "ownership-backed"** (`materialize.go:2050`). It was correct only because Devin
  happens to be both. **FIXED** by bead agent-marketplace-5exj.29: the fence now
  keys off `harness.TargetInfo.OwnershipReconciledAggregates`, the two properties
  are documented as independent on the fields themselves, and
  `materialize.TestAggregateProtectedPathsKeysOffOwnership` proves a hypothetical
  ownership-backed non-symlink target is fenced. Setting that one field is all a
  migrating target needs; without the fix its aggregates would have been pruned
  by `stripManagedEntriesFromFile` behind the reconciler's back.
- **(g) `MergeWithExistingJSON` deletes a whole container key** when all of its
  array members were managed (`merge.go:112`). For `plugin.json`, removing the
  last managed skill removes the `skills` key entirely rather than leaving
  `[]`. Cosmetic, but it is a shape change to a manifest the user may be
  reading.
