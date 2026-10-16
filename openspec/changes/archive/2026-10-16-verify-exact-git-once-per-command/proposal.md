## Why

Every time a command opens an exact snapshot it starts six Git processes: two
for the repository-local config check, plus object format, `HEAD`, worktree
root, and `ls-tree`. Every operation on the shared object store re-runs the
config cleanup on top of that. One command opens the same exact snapshot
several times, so a single `sync` repeats identical checks, and slow process
creation dominates its runtime. One command test, for example, starts 1,248
Git processes. Its Go process spends about 3 s of CPU in a 27 s run; the rest is
spent waiting on Git. Under concurrent load the same checks push command tests
past their 120 s deadline.

## What Changes

- Each command verifies each exact snapshot and object store before it first
  uses them, and does not repeat that verification for the rest of the command.
  Each TUI action, such as an inspection reload, a sync, or an update, counts as
  its own command for this purpose. A failed verification is never reused, and
  a snapshot republished by the same command is verified again.
- Repository metadata that Git writes in its canonical form is checked by
  reading the files directly. Anything else falls back to the existing Git
  subprocess checks, which remain the only path that can reject. The
  tracked-tree comparison still uses `git ls-tree`.
- An exact snapshot tampered with between two commands or two TUI actions is
  still rejected by the next one. Repository metadata that the existing checks
  reject is still rejected, whatever its formatting.

No configuration, acquisition lock, cache layout, diagnostic code, or exit code
changes.

## Capabilities

### New Capabilities

- `exact-snapshot-verification`: when exact snapshots are verified within a
  command or TUI action, and which repository metadata is accepted.

### Modified Capabilities

None. The existing scenarios `registry-symlink-containment/snapshot-tracked-tree-is-altered`
and `tui-feedback/untrusted-snapshots-remain-errors` keep their wording and must
keep passing.

## Impact

- Affected code: `internal/cache` (exact-git verification, object-store config
  cleanup, and a verification scope carried in the command context),
  `cmd/agent-marketplace/main.go` (attaching the scope to each CLI command), and
  `cmd/agent-marketplace/tui.go` plus the TUI reload entry points (one scope per
  action).
- No new dependency: the fast path reads files with the standard library.
- Fewer Git processes for every command that reads exact snapshots, in the
  shipped CLI and in the `cmd/agent-marketplace` test suite.
