## Context

See proposal.md for the motivation. The relevant current code:

- `internal/cache/exact_git.go`:
  - `verifyExactGitSnapshotWithContext` runs on every exact snapshot open
    (`AcquireExactGitWithContext`, `LeaseExactGitReadOnlyWithContext`, and
    `openExactGitPathWithContext`).
  - It calls `validateExactGitConfigWithContext` (`git config --local
    --name-only --list` and `git rev-parse --git-path config`), then
    `rev-parse --show-object-format`, `rev-parse --verify HEAD`,
    `rev-parse --show-toplevel`, and `ls-tree -rz --full-tree -r HEAD`, and then
    hashes every tracked file in-process.
- `sanitizeExactGitConfigWithContext` runs before every object-store operation
  in `ResolveExactGitRefWithContext`, `ensureExactGitObject`, and clone. It
  lists config, removes `origin`, removes `FETCH_HEAD` (`rev-parse --git-path
  FETCH_HEAD`), and re-runs the validation.
- Every Git subprocess runs through `execGitContext` with `gitEnv()`. That
  strips every `GIT_*` variable except the SSH/SSL/askpass/proxy allowlist, and
  disables `fsmonitor`, hooks, and the `ext`/remote protocols.
- Five call sites reach these functions: `cmd/agent-marketplace/lock.go`
  (resolve, acquire, `WithAcquiredExactGit`, `WithExactGitReadOnly`) and
  `internal/pipeline/lockview.go` (`LeaseExactGitReadOnlyWithContext`).
- `cmd/agent-marketplace/main.go` already attaches a per-command cache to the
  Cobra context (`pipeline.WithTransientOrganizationMarketplaceLockCache` in
  `PersistentPreRunE`). The root TUI runs its whole session under that same
  context.
- Snapshot configs this repository writes (`git init`, then sanitize) have
  exactly this form: `[core]` followed by tab-indented `key = value` lines.
  SHA-256 repositories add `[extensions]` with `objectformat = sha256`. A
  detached snapshot `HEAD` is the bare hex OID plus a newline.

## Goals / Non-Goals

**Goals:**

- Run each exact snapshot and object-store verification at most once per
  command or TUI action.
- Remove five of the six Git processes from each remaining snapshot
  verification, and from the object-store config cleanup, whenever the
  metadata is in canonical form.
- Accept and reject exactly the same repositories as today.

**Non-Goals:**

- Replacing the `git` CLI, including for clone, fetch, push, and network
  transport, or adding go-git.
- Reading Git objects in-process. `ls-tree` stays a subprocess.
- Persisting verification results across processes or commands.
- Changing the tracked-tree hash walk, quarantine, locking, or GC.
- Test-suite fixture or matrix changes. Those are separate work.

## Decisions

### D1: Verification scope carried in the context, opt-in

- `cache.WithExactGitVerificationScope(ctx) context.Context` attaches a
  mutex-guarded set. It returns `ctx` unchanged when a scope is already
  present, following the transient lock-cache pattern.
- Set keys:
  - `(snapshot path, algorithm-qualified OID)` for snapshots
  - `(object-store path)` for sanitized stores
- Only successful results are recorded.
- When no scope is present, behavior is exactly today's. This keeps library
  callers, `serve`, and tests that do not opt in on the strict path.

Alternatives considered:

- A process-global memo. It would make the TUI session and in-process test
  commands reuse verification across actions, which violates the requirement.
- Persisting a marker in the cache. That adds state and new trust questions
  for little extra gain.

### D2: Where scopes begin

- **CLI commands:** `PersistentPreRunE` attaches a scope unless the root TUI
  will run (`rootTUI` is already computed there).
- **TUI:** each action attaches a fresh scope to the context it passes to the
  pipeline:
  - inspection load/reload
  - the `Sync` and `Update` callbacks in `cmd/agent-marketplace/tui.go`
  - `prepareTUILocks`
- A TUI action's scope ends when the action returns. The next reload therefore
  re-verifies, as `tui-feedback/untrusted-snapshots-remain-errors` requires.

### D3: Lease and existence checks stay on every open

A scope hit skips only `verifyExactGitSnapshotWithContext`. These still run on
every open:

- the shared namespace lease, so GC still excludes the snapshot during use
- `validateExactGitDirectory` and `requireExactGitSnapshot`

### D4: Invalidation

- Inside `AcquireExactGitWithContext`:
  - Quarantining a snapshot path removes its key before the rename.
  - A successful `publishExactGitSnapshotWithContext` records the key, because
    publish already ran a full verification on the staged directory.
- An object-store key is removed when the store is cloned or initialized, and
  it is recorded after a successful sanitize.
- Fetches with an explicit URL and `--no-write-fetch-head` add no config or
  `FETCH_HEAD`, so a sanitized store stays sanitized for the rest of the
  command.

### D5: Fast path that can only accept

`readExactGitLocalState(repo string, bare bool) (state, ok bool)` returns
`ok == false` ("not sure") for anything outside the canonical shape. Callers
then run the existing subprocess function unchanged. The fast path never
returns a rejection.

It returns `ok == true` only when all of the following hold:

1. **Git directory location.**
   - Bare store: the repository root.
   - Snapshot: `<repo>/.git`, which `Lstat`s as a real directory (not a
     symlink, not a `gitdir:` file) and contains no `commondir` file.
2. **Ownership and file type.** The repository root and the git directory are
   owned by the effective UID, so Git's `safe.directory` ownership check would
   pass. `config` is a regular file owned by the effective UID.
3. **Config syntax.** Every line of `config` is one of:
   - exactly `[core]`
   - exactly `[extensions]`
   - `\t<key> = <value>`, where `<key>` is a lowercase allowlisted key for the
     current section and `<value>` matches `^[a-z0-9]+$`

   There are no duplicate keys, no blank or comment lines, no CR bytes, and
   the file ends with a trailing newline.
4. **Object format.** With no `extensions.objectformat`, the format is sha1 and
   `core.repositoryformatversion` must be `0`. With
   `extensions.objectformat = sha256`, it must be `1`. Any other combination is
   "not sure".
5. **HEAD (snapshots only).** `HEAD` is exactly a lowercase hex OID of the
   algorithm's length followed by `\n`.
6. **Worktree root (snapshots only).** Given 1 and 3, the worktree root is
   `repo`, because `core.worktree` is not allowlisted and `gitEnv()` strips
   `GIT_WORK_TREE`/`GIT_DIR`. This is the one behavior the differential tests
   must confirm against real Git, including `core.worktree` in a temporary
   global config. If Git honors it, the fast path keeps `rev-parse
   --show-toplevel`.

**Effect on the existing checks:**

- `verifyExactGitSnapshotWithContext` uses the fast path for config, format,
  `HEAD`, and worktree root, keeps `ls-tree`, and keeps the hash walk.
- `sanitizeExactGitConfigWithContext` uses the fast path to skip config
  listing and `--git-path` when there is nothing to remove. A store that has
  `origin` (right after clone) takes the subprocess path, which removes it.
- `FETCH_HEAD` removal uses `<gitdir>/FETCH_HEAD` when the fast-path location
  is known.

Because every fast-path acceptance is a canonical form of metadata the
subprocess path also accepts, the set of accepted repositories cannot grow. A
non-canonical repository costs the old subprocess price; it is not refused.

Alternatives considered:

- A strict parser that rejects directly. It risks refusing Git-valid
  repositories written by other Git versions, which `sync --check` would then
  fail on.
- go-git for config and objects. It is a new dependency, and object reading is
  not needed for the main saving.

## Risks / Trade-offs

- **[Risk] The fast path accepts metadata Git would interpret differently.**
  → Acceptance is limited to the exact form verified above. A differential
  test table and a fuzz target require that every fast-path acceptance is
  also accepted by the subprocess path with identical object format, `HEAD`,
  and worktree root.
- **[Risk] Tampering during a single command is not detected by that command.**
  → The cache tree is user-private (0700/0600), and today's verification already
  leaves a time-of-check/time-of-use gap before consumption. The next command
  or TUI action re-verifies. The spec now states this boundary explicitly.
- **[Risk] A missed invalidation point reuses a stale positive result after
  republish.** → Keys are recorded only after successful verification or
  publish and removed before quarantine. A unit test republishes within one
  scope and asserts re-verification.
- **[Risk] A TUI action passes a long-lived context without a fresh scope.** →
  The TUI's scope wiring is covered by the reload scenario test, which tampers
  between two reloads in one TUI session.
- **[Trade-off]** Git-written configs that are canonical but contain an
  allowlisted key with a value outside `^[a-z0-9]+$` take the slow path. That
  costs performance only.

## Migration Plan

- No data migration. Rollback is reverting the commits.
- The work lands as two commits in one pull request:
  1. the scope and its wiring (D1–D4)
  2. the fast path (D5)

  Either can be reverted alone.
