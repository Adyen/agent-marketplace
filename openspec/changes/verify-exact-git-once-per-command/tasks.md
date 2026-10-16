## 1. Baseline measurement

- [ ] 1.1 Record the baseline with a test-only `git` wrapper first on PATH that
      appends one line per invocation:
      - the Git invocation count for `TestSyncIncompleteSatisfactionKeepsLegacyBehavior`,
        run in-process with `-command-tests-in-process`
      - the wall time of `go test -short -count=1 ./cmd/agent-marketplace`

      Store both numbers in the pull request description.

## 2. Verification scope (design D1–D4)

- [x] 2.1 Add the context-carried verification scope to `internal/cache` and
      consult it in `verifyExactGitSnapshotWithContext` callers and
      `sanitizeExactGitConfigWithContext`. Verify with new `internal/cache`
      tests, which count Git invocations through a PATH wrapper:
      - two opens in one scope run the snapshot verification once
      - opens without a scope verify every time
      - a failed open in a scope is fully re-verified on the next open
      - quarantine and republish within one scope re-verifies
      - tampering between two scopes is rejected
- [x] 2.2 Invalidate store keys on clone/init and record them after sanitize.
      Verify with an `internal/cache` test in which resolve then acquire within
      one scope sanitizes each store once and re-sanitizes after a fresh clone.
- [x] 2.3 Attach a scope in `PersistentPreRunE` for non-TUI commands. Verify
      with the command tests in 4.1 and 4.2.
- [x] 2.4 Attach a fresh scope per TUI action: inspection load/reload, the
      `Sync`/`Update` callbacks, and `prepareTUILocks`. Verify with the TUI test
      in 4.3.

## 3. Accept-only fast path (design D5)

- [x] 3.1 Implement the canonical-form local-state reader and use it in
      snapshot verification and store sanitize, falling back to the existing
      subprocess functions whenever it is not sure. Verify with a
      differential table test. It runs each fixture through both paths and
      asserts the fast path either declines or reports the same object format,
      `HEAD`, and worktree root as the subprocess path, and that the subprocess
      path accepts it. Fixtures:
      - canonical sha1 and sha256
      - CRLF, blank and comment lines, uppercase or quoted keys or values,
        duplicate keys
      - `include.path`, `core.worktree`
      - `.git` as a file, `.git` as a symlink, a `commondir` file
      - symbolic `HEAD`, a short `HEAD`
      - a missing trailing newline
- [x] 3.2 Add `FuzzExactGitLocalState` over config bytes with the same
      implication property. Its seed corpus runs in ordinary `go test`. Verify
      that `go test ./internal/cache -run FuzzExactGitLocalState` passes.
- [x] 3.3 Confirm against real Git whether `core.worktree` in a temporary
      global config changes `rev-parse --show-toplevel` for a repository with a
      `.git` directory:
      - if it does not, the fast path derives the worktree root
      - if it does, keep the subprocess call for the worktree root

      Record the result in the test name and an assertion.

## 4. Scenario tests

- [x] 4.1 Add a command test annotated
      `// spec: exact-snapshot-verification/snapshot-tampered-between-commands-is-rejected-by-the-next-read-only-command`.
      It runs `sync --locked`, modifies a tracked snapshot file, and runs
      `sync --check`, asserting:
      - a nonzero exit with `lock.snapshot-unavailable`
      - byte-identical config, acquisition lock, ownership state, and output

      If the current diagnostic differs, stop and reconcile the spec before
      continuing.
- [x] 4.2 Add a command test annotated
      `// spec: exact-snapshot-verification/snapshot-tampered-between-commands-is-repaired-by-the-next-sync`.
      It asserts that a second `sync --locked` succeeds, that the snapshot's
      tracked tree matches the locked commit afterwards, and that the
      acquisition lock is byte-identical.
- [x] 4.3 Add a TUI test annotated
      `// spec: exact-snapshot-verification/snapshot-tampered-between-tui-actions-is-reported-on-reload`.
      It verifies, tampers, and reloads in one TUI session, and asserts an
      error rather than in-sync.
- [x] 4.4 Add command tests annotated
      `// spec: exact-snapshot-verification/equivalent-non-canonical-repository-metadata-is-accepted`
      and
      `// spec: exact-snapshot-verification/disallowed-repository-configuration-is-rejected-however-it-is-written`.
      Each rewrites the exact snapshot's `.git/config` before `sync --check`.
- [x] 4.5 Run `go test ./internal/cache ./internal/pipeline ./internal/tui -count=1`,
      the new command tests by name, and the existing
      `registry-symlink-containment/snapshot-tracked-tree-is-altered` and
      `tui-feedback/untrusted-snapshots-remain-errors` tests, and verify that all
      pass.

## 5. Measurement and gates

- [ ] 5.1 Repeat the 1.1 measurement on the final commit and record the before
      and after numbers in the pull request description.
- [ ] 5.2 Run `make spec-check`, `make validate`, and `make tui-qa`, and verify
      that all pass.
- [ ] 5.3 Run `openspec archive verify-exact-git-once-per-command --yes` as the
      final commit of the pull request.
