## Context

Claude Code is a legacy single-root harness target: its machine root is
`~/.claude`, computed from the home directory, and its machine MCP aggregate is
the `mcpServers` member of `~/.claude.json`, which sits *outside* that root. The
aggregate is declared with a scope-root flag that means "this file lives at the
scope anchor (repository root or HOME), not under the target root", and several
consumers key behavior off that flag or off root inequality: the prune fence
leaves scope-root aggregates unprotected because they are assumed to be outside
the pruned root, backup mapping skips an aggregate whose root equals the default
root, and the native hook route checks whether an aggregate shares the hook
file's directory.

`CODEX_HOME` (partition-level, declared on the native layout) and
`OPENCODE_CONFIG_DIR` (installation adapter) are the two existing relocation
precedents. Both validate that the value is absolute and fail with
`<VAR> must be absolute`; both leave repository scope alone, except
`OPENCODE_CONFIG_DIR`, which also relocates repository resources and therefore
needed boundary and preflight special cases.

A prototype that only switched the two root computations installed resources
under `CLAUDE_CONFIG_DIR` but silently lost the MCP entry: once `.claude.json`
moves inside the root, the flag-based assumptions above no longer hold.

## Goals / Non-Goals

Goals:

- Honor an absolute `CLAUDE_CONFIG_DIR` for every machine-scope Claude Code
  destination, including `.claude.json`, through every command that resolves
  that root.
- Keep `.claude.json` — Claude Code's state and credentials file — safe when it
  lives inside a root that materialization prunes.
- Refuse, rather than guess, when the relocated root collides with the
  repository Claude Code root.
- Keep the default (unset) behavior byte-identical, including the existing
  repository-is-HOME handling.

Non-goals:

- Coalescing repository and machine Claude Code plans that share a root, as the
  Codex preflight does. Refusal is sufficient for this change.
- Migrating or pruning output that an earlier run left in `~/.claude` or
  `~/.claude.json`. `CODEX_HOME` does not do this either.
- Relocating repository-scope destinations. Claude Code keeps project files in
  the repository regardless of the variable.
- A new native-layout descriptor field, an installation adapter for Claude
  Code, or any change to other harness targets.

## Decisions

### Resolve the variable in the legacy single-root path, not in the descriptor or an adapter

Three routes were available. The native-layout `MachineRootEnv` is rejected by
the library validator for single-root targets on purpose: the descriptor
declares topology, and resolving an environment variable is consumer policy.
It also cannot express that the variable moves a scope-root aggregate. An
installation adapter would require adding Claude Code to the adapter registry,
whose composition root assumes ownership-reconciled aggregates and a declared
skill path pattern; Claude Code is marker-reconciled and deliberately leaves
the pattern unset so the compiler's frontmatter is not re-injected. Migrating
Claude Code to the adapter model is a separate change.

The chosen route mirrors `openCodeMachineRoot`: one helper returns the Claude
Code machine root for a home directory, honoring an absolute
`CLAUDE_CONFIG_DIR` and returning `CLAUDE_CONFIG_DIR must be absolute`
otherwise. It feeds the legacy install-root table (which has no error return
and keeps the default on an invalid value so that unrelated callers such as
target listing keep working), the legacy branch of destination resolution
(which reports the error), the machine-scope branch of the Claude aggregate
destination, and explicit-home detection, where the variable takes precedence
over the supplied home because Claude Code itself ignores `~/.claude` when the
variable is set. The variable is read at resolution time, as `CODEX_HOME` is,
so a process-wide environment change in tests is observed.

### The aggregate root follows the machine root

In machine scope the Claude MCP aggregate's root is the resolved machine root
rather than HOME, so `.claude.json` is `$CLAUDE_CONFIG_DIR/.claude.json`. With
the variable unset the resolved root is `~/.claude` and the aggregate root
stays HOME, which is the current layout. The destination ID stays
`machine-mcp`; repository scope is untouched.

### The prune fence protects by location, not by flag

The aggregate prune fence adds a scope-root aggregate's file to the protected
set whenever the aggregate's resolved root names the same location as the
default destination root. Only then can the marker-strip prune walk reach the
file. This keeps the unset layout unchanged and is what makes the relocated
`.claude.json` safe; the "pruning preserves the configuration file" scenario is
the byte-equality proof.

### Backup keeps `.claude.json` as an explicitly mapped archive entry in both layouts

Today the Claude machine archive carries `.claude.json` under
`.agent-marketplace-native/machine-mcp/.claude.json` because its root differs
from the default root. If the mapping were keyed on root inequality, the
relocated layout would instead archive the file as an ordinary root-relative
entry, so archives would depend on the environment they were made in and an
archive from the default layout would fail to restore with the variable set
("unmapped native aggregate entry"). The mapping is therefore keyed on the
scope-root aggregate itself, and managed-path collection excludes mapped files
from the ordinary relative paths so the file is archived once. Restore resolves
the mapping at restore time, so archives made under either layout publish into
the current effective location.

### Overlap is refused, mirroring the OpenCode rule

When the variable is set and both Claude Code output keys are active, a small
preflight runs before the first directory is created, in the same position as
the Codex and OpenCode preflights. It compares the resolved machine root with
`<repo>/.claude`: identical location, or ancestor/descendant, using the same
location-identity comparison the Codex preflight uses (existing paths by
identity through the nearest existing ancestor; missing paths by case- and
NFC-insensitive spelling). On overlap it emits
`materialize.destination.overlap` for target `claude`, names both paths, hints
at moving `CLAUDE_CONFIG_DIR` or the repository, removes both Claude Code keys
from the run so nothing is written or pruned in either scope, and lets other
targets continue. `sync --check` reports the same diagnostic. The location
helpers are generalized from the Codex implementation rather than duplicated.

The unset case runs no preflight, so the existing repository-is-HOME behavior
for legacy targets is unchanged. A configuration directory inside the
repository but disjoint from `<repo>/.claude` is permitted; it is unusual, but
its managed paths do not intersect repository output.

### Detection follows the same resolver; the TUI first-run dialog gets a journey

`init` and the TUI first-run dialog both detect installed harnesses through the
shared destination resolver, so no TUI file changes. The pre-selected harness
list is nevertheless user-visible TUI behavior, so a real-terminal journey sets
`CLAUDE_CONFIG_DIR` to a sibling of `~/.claude`, creates only that directory,
and asserts Claude Code is pre-selected. The TUI QA isolated environment also
pins the variable to `~/.claude` by default, as it pins `CODEX_HOME`, so a
developer's shell cannot change journey outcomes.

### Test sandboxes isolate the variable everywhere HOME is isolated

`testutil.IsolatedHome` unsets `CLAUDE_CONFIG_DIR`, covering every package
whose `TestMain` uses it. The process-isolated command runner, its environment
key lists, the in-process `TestMain`, the E2E framework environment and its
sandboxed-key list, and the TUI QA environment pin the variable explicitly.
Fixtures that need a relocated root set it per test with `t.Setenv`.

## Risks / Trade-offs

- `.claude.json` holds Claude Code's OAuth and account state. Any defect in the
  fence or in aggregate reconciliation now risks that file rather than a
  plugin directory. Scenarios assert byte equality of the untouched file and
  preservation of unrelated members on every mutation path, including
  `remove` of the last MCP server and `backup restore`.
- The machine materialization manifest lives under `~/.agent-marketplace` and
  is shared across roots. Toggling the variable between runs re-installs at the
  new root and leaves the previous root's files in place; `sync --check` does
  not report them. This matches `CODEX_HOME` and is documented rather than
  changed.
- Running `sync --check` from a shell without the variable reports drift
  against `~/.claude`. Expected, and the same as `CODEX_HOME`.
- Refusing overlap instead of coalescing means a user who points
  `CLAUDE_CONFIG_DIR` at `<repo>/.claude` cannot use both scopes. The
  diagnostic names the fix; coalescing can follow as its own change if needed.
- The `hook-resources` and `hook-native-location-tolerance` tables keep
  `~/.claude/...` as the default spelling. The new capability defines the
  relocation explicitly rather than editing those tables, which an in-flight
  change already rewrites.

## Open Questions

- Should a configuration directory inside the repository but outside
  `<repo>/.claude` be refused as well, for predictability? Proposed: no,
  because its managed paths are disjoint and Claude Code itself accepts it.
- Should an invalid (relative) value be surfaced by `backup` target listing,
  which today only enumerates targets? Proposed: no; `backup` for Claude Code
  resolves the root through the same path and reports the error there.
