# Serializing `internal/ownership` per canonical root

Design for bead `agent-marketplace-9gmb` (P0). This is a **design document**, not
an implementation: no production code changes with it. Every rule quoted below
is quoted from `internal/ownership/doc.go`,
[`2026-08-03-devin-first-class-harness.md`](./2026-08-03-devin-first-class-harness.md)
("the Devin plan"), or read directly off the code at the line references given.
Nothing here invents a second set of ownership semantics.

## Promotion boundary

This plan extends the Devin plan's "Journaled generations" section with the one
thing it never specified: what happens when **two processes** run a generation
against the same canonical root at the same time. It supersedes nothing. It
changes no on-disk format, no diagnostic severity, and no ownership rule.

## Objective

`internal/ownership` takes no lock of any kind and `Store.Apply` is not a
compare-and-swap. The write path is a read-modify-commit with no serialization:

| Step | Where |
|---|---|
| open the store | `internal/materialize/devin.go:429` |
| **recover an in-flight generation** (a WRITE) | `devin.go:439` |
| read the aggregate | `devin.go:469` |
| read the ownership record | `devin.go:525` (`store.Proof(rel)`) |
| patch, then commit | `devin.go:563` (`store.Apply`) |
| — inside `Apply`: | `journal.go:126` `s.Load()` → `:133` `generation = current.Generation + 1` → `:204` `writeStaged` → `:221` `os.Rename` publish → `:254` `commitRecord` |

Two writers on one canonical root therefore publish the file and the record at
different instants. Verified reachable from plain `sync` at 60/60, and
pre-existing (byte-identical on the shipping branch).

The observed consequences are already catalogued on the bead and are not
re-derived here: a durable record that no longer describes the file (100%), a
spurious `materialize.devin.aggregate-write` error, entries becoming permanently
unmanaged (~40–55%), and a later `sync` classifying the survivor `OwnedDrifted`
and rewriting it with a misleading "was edited by hand" warning.

> **CORRECTION (B4 adversarial review, `agent-marketplace-npwr`).** This section
> originally claimed that user hand-written bytes are *not* destroyed by plain
> sync, and that the outcome is scoped to `doctor --repair`'s conflict-replace.
> **That claim was false**, and it was false for a reason unrelated to locking:
> `devinHookIdentity` was lossy, so two hooks differing only in a payload key the
> identity ignored could share an identity digest. Plain `sync` then classified the
> hand-written twin as `OwnedDrifted` and replaced it **with no confirmation at
> all**. This was fixed by `agent-marketplace-2a41`: hook identity is now the
> event name plus the digest of the whole canonical rule value, so two different
> values always have different identities. The reproduction in
> `internal/materialize/devin_hook_identity_known_defect_test.go` is unskipped and
> now asserts the hand-written twin is preserved.
>
> This does not change the locking work in this document — the race and the
> lossy identity were independent defects — but it did mean the severity
> scoping used to justify the rollout order was wrong, and no later reader
> should rely on the original sentence.

The objective is to make every mutating path against one canonical root
**mutually exclusive across processes**, without a new module dependency,
without wedging a future sync when a process is killed, and without weakening a
single fail-closed invariant.

## Non-goals

- Fixing the shared staging filename (`stagedPathFor`, `journal.go:328`). That
  is `agent-marketplace-rhn5`. See [Interaction with rhn5](#interaction-with-rhn5).
- A second durable store, a second journal, or a second backup scheme.
- Changing the state schema (`stateVersion = 1`) or the journal schema
  (`journalVersion = 1`).
- Locking anything outside `internal/ownership`'s aggregate reconciliation.
  Skill symlinks, prune walks, and config edits are out of scope.
- Windows support. See [6. Platform policy](#6-platform-policy).

---

## What reading the code changed about the problem statement

Six findings that the bead description does not contain. Two of them change the
design; one is a trap that would silently regress a user-visible behaviour.

### F1. `Recover` is a writer, and it is outside every proposed section

`Store.Recover` (`journal.go:429`) is called once per root at `devin.go:439`,
before the per-file loop. It is not a read: `rollForward` calls `commitRecord`
(`:479`), `rollBack` and `abandon` call `removeStaged` and `clearJournal`
(`:495`, `:511`). So process B's `Recover` can roll forward, roll back, or
**abandon** process A's genuinely in-flight generation — deleting A's staged
file out from under A's rename, or committing A's `NextRecord` while A is still
staging. This is a strictly worse failure than the one the bead describes, and
it means the critical section **must** open before `Recover`, not before `Apply`.

### F2. The journal is a single slot per root, and the generation counter is per root

`Store.JournalFile()` is `…/ownership/<rootKey>/journal.json` — one slot, not one
per aggregate file. `Record.Generation` is likewise one counter covering **all**
files of the root (`state.go:334`, `nextRecord` copies `current.Files` wholesale).
Two writers touching *different* aggregate files of the same root therefore
collide just as hard as two writers touching the same file. The bead frames the
defect per-file; it is per-root. This is the decisive argument for
[per-root granularity](#5-scope-granularity).

### F3. `doctor --repair`'s in-flight guard is itself a TOCTOU

`RepairFile.requireCompleteGeneration` (`devin_repair.go:765`) reads the journal
and returns; `Apply` writes the journal some microseconds later. Without a lock
the guard proves nothing. `TestAdversarialConflictReplaceDoesNotDiscardAnIncompleteGeneration`
(`devin_conflict_adversarial_test.go:370`) passes today only because it is
single-threaded.

### F4. A lock file inside `recordDir` would silently regress first-run reporting

`Store.EverRecorded` (`state.go:246`) answers "was a record expected here?" by
`os.Lstat`-ing `recordDir()`. If acquiring a lock creates that directory, then
`EverRecorded` answers yes on a first-ever run, `missingStateError` returns plain
`ErrStateMissing` instead of `ErrStateUninitialized`, and `Store.Proof`
(`state.go:386`) escalates a debug fact into a user-facing
`ownership.state-missing` **warning** on every fresh install. The lock file must
therefore live outside `ownership/<rootKey>/`. See
[Where the lock lives](#where-the-lock-lives).

### F5. `Open` is contractually side-effect free

`Open`'s doc comment (`store.go:109`) states it "does not create anything on
disk, so calling it on a read-only path (for example `status`) is side-effect
free". `status`, `list`, `sync --check`, the TUI background refresh, and
`DevinRepairPreview` all open stores. Acquisition must therefore be an explicit
call, never a side effect of `Open`.

### F6. Every writer already funnels through two places

Only `internal/materialize/devin.go` and `internal/materialize/devin_repair.go`
call the store (`grep -rl internal/ownership` on non-test code returns
`pipeline.go`, `roots.go`, `materialize.go`, and `backup.go` as well, but those
reference it only in comments and for `RootKey`). `sync`, `install`, `remove`,
`update`, `deinit`, and TUI sync/update all reach the aggregate path through
`materialize.Run` → `reconcileDevinAggregates`. `doctor --repair` reaches it
through exactly three methods: `RepairFile.Adopt` (`devin_repair.go:461`),
`RemoveOrphan` (`:531`), `ReplaceConflict` (`:637`). **Four call sites total.**
The change does not fan out across writers, which is why the decomposition below
is small.

---

## 1. Mechanism

### Options

| Option | Serializes across processes? | Survives SIGKILL? | New dependency | Critical section can span reads outside `Apply`? |
|---|---|---|---|---|
| **A. Advisory OS file lock** (`flock(2)`) | yes | yes — the kernel drops the lock when the fd closes or the process dies | **none** (`syscall.Flock` is stdlib) | yes |
| **B. `O_EXCL` lock file with owner metadata** | yes | **no** — needs a stale-lock policy, PID liveness, a threshold, and a breaking protocol | none | yes |
| **C. CAS on `Apply`** (generation and/or prior-digest precondition; `agent-marketplace-y1id`'s `Mutation.ExpectPriorDigest`) | detects, does not prevent | n/a | none | **no** |

### Why CAS alone is not sufficient

CAS turns a corruption into a retry, which is a real improvement — but only for
the window it covers. Three things break it here:

1. **The critical section is wider than `Apply`.** The aggregate is read at
   `devin.go:469` and the record at `:525`, and the patch is computed from both.
   A CAS inside `Apply` can reject a stale record, but by then the caller has
   already burned a mandatory safety backup and, in the repair path, already
   consumed a human's per-entry confirmation. The retry would have to re-prompt.
2. **`Recover` has nothing to CAS against** (F1). It is a write that decides
   between roll-forward, roll-back, and abandon by comparing digests; two
   concurrent `Recover`s on one journal have no precondition to check.
3. **A rejected CAS is still a user-visible failure.** The bead's "one of two
   terminals exits non-zero incomprehensibly" symptom does not go away; it
   changes its error text. Blocking briefly and succeeding is the outcome a
   save-hook racing a human actually wants.

### Recommendation

**Option A, `flock(2)` via stdlib `syscall`, plus Option C as a portable
backstop** (a separate, later bead — see B5).

`flock` is recommended over an `O_EXCL` lock file for one reason that dominates
everything else: **the kernel releases it**. Section 4 of this document, which
would otherwise be the hardest part of the design (PID liveness across a PID
namespace, clock skew on the timestamp, a break protocol that cannot race
itself), collapses to "there is no such thing as a stale flock". A crashed,
`kill -9`'d, or OOM-killed process cannot wedge a future sync.

CAS is kept as a **defence-in-depth backstop for the one case flock cannot
cover**: a filesystem that refuses `flock` (see
[Filesystems that refuse to lock](#filesystems-that-refuse-to-lock)). CAS needs
no kernel support, so it is exactly the right complement.

### Dependency verdict: zero new modules

Established by reading `go.mod`, `go.sum`, the `Makefile`, and `e2e/go.mod`:

- **Go's stdlib does have portable-enough file locking for our platforms.**
  `syscall.Flock(fd int, how int) error` plus `syscall.LOCK_EX`, `LOCK_NB`, and
  `LOCK_UN` exist on darwin, linux, and every BSD. They do not exist on Windows,
  plan9, or js/wasm — which is why the `//go:build unix` split in section 6 is
  required, not why a dependency is.
- **`golang.org/x/sys` is now a direct repository dependency for other
  filesystem operations.** Ownership locking still uses `syscall.Flock` because
  `x/sys/unix.Flock` is the same syscall with a nicer errno type and would add no
  capability here.
- **The `e2e` module is deliberately dependency-free.** `e2e/go.mod` has a
  `module` line, a `go` line, and **no `require` block at all**. It will not
  import `internal/ownership` (it drives the built binary as a subprocess), but
  keeping the lock stdlib-only means that invariant stays unconditional rather
  than conditional on "as long as e2e never needs it".
- **`CGO_ENABLED=0`** on every build target (`Makefile:28`, `:63`, `:67`, `:71`).
  `syscall.Flock` is a pure syscall; it is unaffected.

**Verdict for ownership locking: use `syscall` and do not add a third-party lock
library.** The repository's direct `golang.org/x/sys` dependency serves other
filesystem operations; switching this lock implementation to it would add no
capability.

### Probe result (throwaway, not committed)

A scratch program under `/tmp` confirmed the three properties the design leans on,
on this machine's `GOOS`:

```
same-process second fd non-blocking acquire err: resource temporarily unavailable  (EWOULDBLOCK)
blocking acquire correctly waits while the other fd holds the lock
acquired after release: <nil>
```

The first line is the important one: **two open file descriptors on the same path
contend even within one process**, because a `flock` is associated with the open
file *description*, not the process. That is what makes the two-goroutine
acceptance test in section 7 a genuine test of the mechanism rather than a test
of an in-process mutex.

### Where the lock lives

```
$XDG_STATE_HOME/agent-marketplace/
├── ownership/<rootKey>/            ← state.json, journal.json   (UNCHANGED)
├── safety-backups/<rootKey>/…      ← mandatory raw-byte backups (UNCHANGED)
└── locks/<rootKey>.lock            ← NEW: a zero-to-small advisory lock file
```

A **sibling** of `ownership/` and `safety-backups/`, not a child of
`ownership/<rootKey>/`. This is forced by F4: `Store.EverRecorded` reads
`recordDir()` existence as durable evidence, and a lock file created there would
turn every first run's silent `ErrStateUninitialized` into a user-facing
`ownership.state-missing` warning.

It lives in the **state root, not the install root**, for three reasons: the
install root can be read-only (see
`TestAdversarialConflictReplaceLeavesTheFileIntactWhenTheStagedWriteFails`,
which chmods it to `0555`); a lock file inside `.devin` would be a new file in a
directory the user sees and the prune walk scans; and the thing being serialized
is the *state*, which is keyed by `rootKey` anyway.

`Store.EverRecorded` must be left alone and a regression test must pin that
`locks/` does not count as evidence.

Lock-file **contents** are diagnostics-only: PID, hostname, `os.Args[0]`, and an
RFC3339Nano acquisition timestamp, written *after* the lock is held. Nothing ever
reads them to make a decision. Writing them is best-effort; a failed write does
not fail the mutation.

### Shape of the API

```go
// internal/ownership/lock.go
type sessionHandle struct{ f *os.File; depth int }

// Lock acquires the per-root advisory lock, blocking up to the configured
// timeout. It is reentrant PER STORE: a nested Lock on the same Store bumps a
// depth counter instead of taking a second fd (a second fd would deadlock
// against the first, as the probe shows).
func (s *Store) Lock() (release func(), err error)

// WithLock runs fn with the lock held.
func (s *Store) WithLock(fn func() error) error
```

`Store.Apply`, `Store.Recover`, and `Store.BackupOnly` each take the lock
internally when the `Store` does not already hold it. That makes the fix correct
even for a caller that forgets to widen its section — the depth counter is what
lets `devin.go` widen the section around `Recover` + N × `Apply` without the
inner acquisitions deadlocking.

Two `Store` values for the same root inside one process hold two fds and
therefore genuinely serialize (probe line 1). That is required by the acceptance
test in section 7, whose two "processes" are two goroutines each holding their
own `Store` from their own `DevinRepairPreview`.

---

## 2. Critical-section boundary

### Sync path — `internal/materialize/devin.go`

The section opens in `reconcileDevinAggregates` immediately after
`ownership.Open` (`:429`) and **before** `store.Recover()` (`:439`), and closes
after the last iteration of the `for _, rel := range rels` loop (`:451`–`:461`).

```
reconcileDevinAggregates(key, outDir, work, dryRun)
  store := ownership.Open(outDir)                       // :429  no lock (F5)
  if dryRun { … no lock at all, read-only … }
┌ release := store.Lock()                               // NEW
│   store.Recover()                                     // :439  (F1: must be inside)
│   for rel := range rels:
│     reconcileDevinAggregateFile(...)                  // :451
│       os.ReadFile(abs)                                // :469
│       store.Proof(rel)                                // :525
│       jsonc patch
│       store.Apply(...)                                // :563  (reentrant no-op acquire)
└ release()
```

`Recover` **must** be inside (F1). The loop must be inside as a whole rather than
per-file, because the journal slot and the generation counter are per root (F2):
releasing between files would let another process interleave a generation into
the same counter.

The section deliberately excludes everything expensive. Git fetch, catalog load,
`resolver.Resolve`, and harness compilation all happen upstream in
`materialize.Run`; what is inside is a handful of `ReadFile`s, jsonc patches, and
fsyncs — single-digit milliseconds for realistic aggregates. This matters for
the timeout in section 3.

**Dry run takes no lock.** `dryRun` already skips `Recover` and returns before
`Apply` (`devin.go:436`, `:556`). `sync --check`, `status`, `list`, and the TUI
background refresh all reach the Devin path only via
`materialize.WithDryRun()`, so they stay side-effect free and, critically, never
create `locks/<rootKey>.lock`.

### Repair path — `internal/materialize/devin_repair.go`

`DevinRepairPreview` (`:244`) is read-only by construction and stays lock-free.

Each of the three mutators takes the lock around its whole body:

| Method | Section opens before | Section closes after |
|---|---|---|
| `Adopt` (`:461`) | `readDevinAggregateState` (`:469`) | `store.Apply` (`:501`) |
| `RemoveOrphan` (`:531`) | `requireCompleteGeneration` | `store.Apply` (`:578`) |
| `ReplaceConflict` (`:637`) | `requireCompleteGeneration` (`:648`) | `store.Apply` (`:689`) |

`requireCompleteGeneration` must be **inside** the section (F3), otherwise the
in-flight-journal guard remains a TOCTOU. So must the candidate re-derivation at
`:653`–`:665`, since that re-derivation is the anti-staleness check and a
concurrent writer between it and `Apply` is precisely the race.

**The lock is never held across a human prompt.** `cmd/agent-marketplace/doctor.go`
prompts between calls; each call is one lock acquisition, one journaled
generation, one release. This is stated as an invariant with a test, because
holding a lock across an interactive `y/N` on `~/.config/devin` would block every
other repo on the machine for as long as the human takes to answer.

### Do other processes' `Proof` / `Classify` reads need the lock?

**No, and they must not take it.** Three independent reasons:

1. Every read is a single `os.ReadFile` of a file that is only ever replaced by
   `atomicWrite` (`store.go:182`: temp + fsync + rename + dir fsync). A reader
   therefore sees the whole previous record or the whole new one, never a mix —
   the same guarantee `Apply`'s `os.Rename` gives for the aggregate.
2. A reader that took the lock would create `locks/<rootKey>.lock` on
   `status`/`sync --check`, violating F5's side-effect-free contract, and would
   make a read-only command block on a long-running sync.
3. A torn *view* is already handled by the fail-closed rule: the worst a reader
   can observe is a record one generation behind the file, which classifies an
   entry `Unmanaged` or `OwnedDrifted`. Both are preserve-don't-prune outcomes
   for a read-only command. The reason the *writer* race is a defect is that the
   writer acts on the stale view; a reader only reports it.

`DevinRepairPreview` is in this category and stays lock-free, which also keeps
off-TTY `doctor --repair` exactly as byte-neutral as it is today.

### Exact functions that change

| File | Function | Change |
|---|---|---|
| `internal/ownership/lock.go` (new) | `Store.Lock`, `Store.WithLock`, `lockPath`, timeout policy | new |
| `internal/ownership/filelock_unix.go` (new) | `tryLock`, `unlock` via `syscall.Flock` | new |
| `internal/ownership/filelock_other.go` (new) | same signatures, return `ErrLockUnsupported` | new |
| `internal/ownership/store.go` | `Store` gains a `session *sessionHandle` field; `Option` gains `WithLockTimeout` and a test-only `WithLockObserver` | edit |
| `internal/ownership/journal.go` | `Apply` (`:113`) and `Recover` (`:429`) acquire if not already held | edit |
| `internal/ownership/safety.go` | `BackupOnly` (`:105`) acquires if not already held | edit |
| `internal/materialize/devin.go` | `reconcileDevinAggregates` (`:428`–`:461`) wraps `Recover` + loop | edit |
| `internal/materialize/devin_repair.go` | `Adopt` (`:461`), `RemoveOrphan` (`:531`), `ReplaceConflict` (`:637`) | edit |
| `pkg/harness-compilation/diag/codes.go` + `codes.md` | four new codes | edit |

---

## 3. Contention behaviour

**Recommendation: block with a bounded wait, then fail that root with a
diagnostic. Default timeout 30 s, overridable, `0` meaning fail fast.**

Rationale. `sync` takes tens of seconds, but the *critical section* takes
milliseconds (section 2) — git fetch and compilation are outside it. So the
realistic contention a save-hook racing a human sees is a few milliseconds of
waiting followed by success, which is exactly the behaviour a save-hook should
have. Failing fast would convert a 5 ms wait into a red terminal, which is the
current symptom with extra steps. Conversely an unbounded wait is unacceptable
because a genuinely wedged holder (a debugger stopped at a breakpoint inside the
section, an NFS stall) would hang a CI job forever.

30 s is chosen as ≫ any legitimate section (ms) and ≪ any CI job timeout, so a
timeout expiring is always a real anomaly worth reporting rather than a tuning
problem.

### Acquisition algorithm

1. `flock(fd, LOCK_EX|LOCK_NB)`. On success, done — the overwhelmingly common
   path, zero latency, no goroutine.
2. On `EWOULDBLOCK`: log at `slog.Info` and start polling with exponential
   backoff from 25 ms to 200 ms, plus ±20 % jitter, until the deadline. Polling
   rather than a blocking `flock` in a goroutine is deliberate: a blocking
   `flock` cannot be cancelled, so its goroutine and its fd would leak past the
   deadline, and a leaked fd holding a lock it acquired after we gave up is the
   one way to build a stale lock out of a mechanism that has none.
3. If the wait exceeded ~1 s, emit `ownership.lock-contended` (info) naming the
   root and the wait duration so a user can see why their sync was slow.
4. On deadline: emit `ownership.lock-timeout` (**error**), write nothing, and
   skip that root's aggregates. Other roots and other targets continue —
   per-root failure isolation, the same shape as "a malformed `mcp_config.json`
   blocks only that file". The run exits non-zero.

### Configuration

`AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT` (a Go duration; `0` = fail fast,
negative = rejected), with the default as a `Default*` constant in
`internal/config/config.go` per the project's Logging/Defaults policy, and
`ownership.WithLockTimeout` as the test seam. No config-file key and no CLI flag
in the first cut: an env var is enough for a CI escape hatch, and a flag would
have to be added to six commands.

### New diagnostic codes

All four must be added to `pkg/harness-compilation/diag/codes.go` **and**
`pkg/harness-compilation/diag/codes.md`; `TestDiagnosticCodes_Documented`
enforces the pair.

| Code | Severity | Summary | Hint |
|---|---|---|---|
| `ownership.lock-contended` | Info | Another agent-marketplace process held this target root's ownership lock; this run waited for it | No action needed; ownership mutations are serialized per canonical target root so two writers cannot publish a file and its record at different instants |
| `ownership.lock-timeout` | **Error** | The target root's ownership lock could not be acquired before the timeout; the aggregates for this root were left exactly as found | Another agent-marketplace process is still holding it; wait for it to finish, or raise `AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT` |
| `ownership.lock-unavailable` | Warning | This filesystem does not support advisory locking, so ownership mutations for this root are not serialized against other processes | Concurrent writers on this root can still race; move `XDG_STATE_HOME` to a local filesystem if two agent-marketplace processes ever run at once |
| `ownership.lock-stale-reclaimed` | Warning | *(reserved — see section 4; not emitted by the recommended design)* | *(reserved)* |

The fourth is listed because a reviewer will ask for it. **The recommendation is
not to add it**: with `flock` there is no such event to report. It is documented
here as the code that an `O_EXCL` design would have needed, so that the absence
is a decision rather than an omission.

A fifth code, `ownership.generation-conflict` (Error), belongs to the CAS bead
(B5) and is specified there, not here.

---

## 4. Stale-lock policy

**Recommendation: there is no stale-lock policy, because `flock` cannot go
stale.**

This is the single largest reason to prefer mechanism A over mechanism B. A
`flock` is owned by the open file description; the kernel drops it when the last
fd referring to that description is closed, which happens on `Close`, on normal
exit, on `panic`, on `SIGKILL`, on OOM-kill, and on container teardown. A
crashed process therefore cannot wedge every future sync — the exact hazard the
bead asks about is designed out rather than mitigated.

Consequently:

- **Staleness detection: none.** No PID liveness check (which would be wrong
  under PID namespaces and PID reuse anyway), no timestamp threshold (which
  would be wrong under clock skew and under a legitimately long section), no
  break protocol (which is itself racy — two processes can decide to break the
  same lock and both proceed).
- **The lock file is never deleted.** It is created once per root and left in
  place forever, empty of meaning. Deleting it is what creates the classic
  unlink race: A holds the lock, B unlinks the path and creates a new file, C
  locks the new file, and A and C are both "holding the lock" on two different
  inodes. `locks/` therefore accumulates one small file per canonical root ever
  used — bounded, tiny, and never garbage-collected by `gc`.
- **The lock file's metadata is advisory only.** PID/hostname/argv/timestamp are
  written for humans reading `ownership.lock-timeout` output. Nothing reads them
  to decide anything, so nothing can be fooled by them.

### Interaction with journal-based crash recovery

The two mechanisms cover disjoint failure modes and, with the section boundary
of section 2, cannot fight:

| | crash *while holding* the lock | crash *not holding* the lock |
|---|---|---|
| journal present | kernel releases lock at death → next writer acquires → `Recover` runs **inside** the section and replays or rolls back per the recorded phase | impossible: the journal is only written inside the section |
| journal absent | nothing to recover; the aggregate is the user's bytes | nothing to recover |

The property that makes this safe is precisely F1's requirement: **`Recover` is
inside the critical section**, so exactly one process ever inspects and resolves
a given journal, and it does so with no other writer able to create a second
one. Today two processes can both `Recover` the same journal; that is a defect
this design closes as a by-product.

`Recover`'s `abandon` path (`journal.go:511`, `ownership.foreign-change`) stays
exactly as written. Under the lock it becomes what it was always documented to
be — evidence that something *outside agent-marketplace* rewrote the aggregate —
rather than something two of our own processes routinely trigger on each other.

### Filesystems that refuse to lock

`flock` can fail with `ENOTSUP`/`EOPNOTSUPP`/`ENOLCK` on some network
filesystems (macOS NFS is the realistic case; Linux maps `flock` onto POSIX locks
over NFS ≥ 2.6.12 and generally works). `XDG_STATE_HOME` on NFS is unusual but an
NFS-mounted `$HOME` is not unheard of in enterprise fleets.

**Recommended behaviour: degrade, warn, and rely on CAS.** On exactly that errno
set, proceed **without** the lock and emit `ownership.lock-unavailable`
(warning). This is not a weakening: it is byte-for-byte today's behaviour, on
the only filesystems where today's behaviour is unavoidable, and it is the
reason CAS (B5) is worth landing as a separate bead — a generation/prior-digest
precondition needs no kernel support and turns the degraded case from silent
corruption into a loud retry.

Any *other* error (permission denied on the state root, ENOSPC) is a hard
failure: those are the same conditions under which the mandatory safety backup
would fail, and `Apply` already refuses a mutation whose backup failed
(`journal.go:154`–`:159`). Flipping the degrade set to "empty" — i.e. refusing
unconditionally — is one constant away if the fleet turns out never to touch NFS.

---

## 5. Scope granularity

**Recommendation: per canonical root. This is not a trade-off; it is forced.**

Finer granularity (per aggregate file) would be *incorrect* today, because the
things being protected are per-root, not per-file (F2):

- `ownership/<rootKey>/journal.json` is **one slot**. Two per-file sections would
  let file X's journal overwrite file Y's in-flight journal — the exact hazard
  `requireCompleteGeneration` exists to prevent, reintroduced by the lock design.
- `Record.Generation` is **one counter for all files** (`state.go:334`). Two
  per-file sections both read generation *N* and both commit *N+1*, so one
  file's `FileState` is silently dropped from the surviving record (the map copy
  at `state.go:335` copies a stale snapshot).
- `commitRecord` rewrites the whole `state.json`, so the last writer wins for
  every file, not just its own.

So per-file locking would require a state-schema change: a per-file journal slot
and a per-file generation counter, i.e. `stateVersion = 2` plus a migration plus
a rewrite of `Recover`'s single-journal assumption. That is a much larger, much
riskier change than the defect warrants, and it would be a **new** correctness
surface rather than a smaller one.

### The `~/.config/devin` cost, stated honestly

`~/.config/devin` is one canonical root shared by every repo on the machine, so
per-root locking serializes the machine-scope aggregate write of every repo's
sync. That is a real cost and it is worth naming precisely rather than waving
away:

- What serializes is the **millisecond** section of section 2, not the sync. Two
  repos syncing concurrently contend for a few ms out of tens of seconds.
- The repo-scope root (`<repo>/.devin`) is a *different* canonical root with a
  *different* `rootKey`, so per-repo work does not contend at all.
- Machine-scope aggregate writes across repos are exactly the writes that
  **must** be serialized: they mutate one shared `~/.config/devin/mcp_config.json`
  under one shared ownership record. Letting them proceed concurrently is the
  defect.

**Verdict: acceptable, and finer granularity is not merely unwarranted but
currently unsound.** If profiling ever shows machine-scope contention matters,
the follow-up is the `stateVersion = 2` per-file-journal bead described above,
filed then rather than now.

Lock-ordering / deadlock: a single `materialize.Run` reconciles roots one
`outputKey` at a time and the section closes before the next begins, so **no
process ever holds two ownership locks at once**. That invariant is what makes
deadlock impossible without a global ordering rule, so it is worth a test
(`WithLockObserver` asserting depth never exceeds one distinct root).

---

## 6. Platform policy

Established from the repo, not assumed:

- `Makefile:10` — `PLATFORMS := darwin-arm64 darwin-amd64 linux-amd64`.
- `Makefile:63`, `:67`, `:71` — the only `GOOS` values that appear anywhere in
  the build are `darwin` and `linux`.
- The release configuration contains no `GOOS`, no `windows`, and no cross-build job.
- `AGENTS.md` "Tech Stack" lists package managers and GitHub releases as the distribution channels.
- No `_windows.go` file and no `runtime.GOOS == "windows"` branch exists in the
  tree.

**This project does not target Windows.** Policy:

| Platform | Support |
|---|---|
| darwin/arm64, darwin/amd64 | **Supported.** `syscall.Flock`, local filesystems. |
| linux/amd64 | **Supported.** |
| other unix (BSD, linux/arm64) | Works by construction (`//go:build unix`), untested, unshipped. |
| Windows, plan9, js/wasm | **Explicitly not supported.** The package still *compiles* — `filelock_other.go` returns `ErrLockUnsupported` and the caller degrades with `ownership.lock-unavailable` — but no serialization is provided and no release is built. |

The `!unix` stub exists purely so `GOOS=windows go build ./...` keeps working for
anyone running an editor's cross-platform analysis; a CI check that compiles for
`windows/amd64` (build only, no test) is cheap insurance and is folded into B1.
If Windows ever becomes a target, the stub is the one file to implement, using
`LockFileEx` — and that is a `golang.org/x/sys/windows` dependency decision to
make *then*, not now.

---

## 7. The skipped repro

`TestAdversarialConcurrentConflictReplacesForgeOwnership`
(`internal/materialize/devin_conflict_adversarial_test.go:672`, sharing
`concurrentConflictReplaceFixture` at `:578`) is skipped with:

> KNOWN DEFECT (agent-marketplace-0w2g review): internal/ownership has no
> per-root lock … Un-skip when Apply serializes per canonical root.

What it actually asserts, read line by line:

1. `concurrentConflictReplaceFixture` seeds one repo-scope `mcp_config.json`
   with two hand-written entries (`aaa`, `zzz`), runs a real sync, then builds
   **two independent previews** — two `DevinRepairPreview` calls, therefore two
   distinct `*ownership.Store` values for one root.
2. It runs `first.ReplaceConflict(0, true)` and `second.ReplaceConflict(1, true)`
   in two goroutines and waits.
3. The assertion: for every `id` in `state.proof.OwnedIDs()`, the record's
   digest must match the digest actually in the file — i.e. no owned entry may
   classify `StatusOwnedDrifted`, because the next `sync` would overwrite a
   hand-written value as drift with no confirmation.

**Does the design make it pass?** Yes, and for the right reason:

- The two goroutines hold two `Store` values, therefore two `os.File` handles on
  `locks/<rootKey>.lock`, therefore two distinct open file descriptions. The
  probe confirms these contend (`EWOULDBLOCK` on the non-blocking attempt from
  the same process). So this is a real test of the lock, not a test that would
  also pass with a `sync.Mutex` package variable.
- With the section of section 2 around `ReplaceConflict`, the loser runs
  *entirely after* the winner. It then re-derives candidates from the current
  file (`devin_repair.go:653`) and calls `recordedEntries()` (`:680`), which
  reads the winner's committed record. Its `Apply` therefore commits
  winner's-entries + its own, at digests that match the file it just staged from.
  `Classify` returns `StatusOwnedClean` for both. Assertion satisfied.
- The loser's own candidate is still valid (a different key, `zzz` vs `aaa`), so
  it proceeds rather than returning `ErrRepairCandidateStale`. If a future
  change made the loser's candidate stale, the test still passes: it asserts
  nothing about *both* replacements succeeding, only that the record never
  claims a digest the file lacks.

**Two caveats that must be written into the bead that un-skips it:**

1. This test is **necessary but not sufficient**. Both racers are goroutines in
   one process, so a lazy fix (a `sync.Mutex` keyed by `rootKey` in a package
   map) would also make it green while leaving the two-terminal defect
   completely unfixed. B6's multi-*process* test is what actually proves the
   fix; this test must not be treated as the acceptance bar on its own.
2. Its sibling, `TestAdversarialConcurrentConflictReplacesKeepUserBytesRecoverable`
   (`:625`), must keep passing unchanged — it pins that the aggregate still
   parses, that ownership never claims an absent entry, and that a retained
   safety backup holds the user's original bytes.

---

## Invariants that must not regress

Each is listed with the mechanism that keeps it true and the test that pins it.

| Invariant | Why the design preserves it |
|---|---|
| Missing or corrupt ownership state fails closed (everything unmanaged, preserve all, prune nothing) | The lock changes *when* `Load` runs, never *what* it returns. `Proof` still yields the zero `Proof` on any error. Add a test that a lock-timeout leaves the root untouched and the next run classifies identically. |
| No `_managed_by` marker in any Devin file | Nothing in the lock path writes to the aggregate. The lock file is in `$XDG_STATE_HOME/locks/`, never in `.devin`. |
| Ownership is never inferred from file bytes | The lock is not evidence of anything. `Proof` remains the only ownership verdict and its inputs are unchanged. |
| `Apply`'s mandatory safety backup happens before any mutation | Ordering inside `Apply` is untouched (`journal.go:144`–`:159`). Acquisition happens *before* `validateMutation`, so a refusal still writes nothing — including no backup, which `TestAdversarialUnsafeMcpKeyFailsClosedWithoutTouchingTheFile` (`:497`) asserts. **Acquisition must not create `recordDir`** (F4). |
| Orphan removal and conflict replace require interactive per-entry confirmation and are never done by `--yes` | `Mutation.Confirmed` and `validateMutation` (`journal.go:297`–`:306`) are untouched. The lock is *inside* each single-entry mutator and never spans a prompt, so the one-consent-one-generation shape is preserved. |
| `Open` is side-effect free; read-only commands touch nothing | Acquisition is an explicit call, never in `Open` (F5). Dry-run and `DevinRepairPreview` take no lock, so they never create `locks/`. Pin with a test that `status` + `sync --check` leave `$XDG_STATE_HOME` byte-identical. |
| First-ever run reports `ErrStateUninitialized`, not a `state-missing` warning | `locks/` is a sibling of `ownership/`, so `EverRecorded` (`state.go:246`) is unaffected. Pin with a test (F4). |

---

## Interaction with rhn5

`agent-marketplace-rhn5` tracks `stagedPathFor(absFile, generation)`
(`journal.go:328`) deriving the staging filename from the file plus the
generation, so two writers that both `Load()` the same record compute the *same*
staged path, B truncates A's bytes, A publishes B's content under A's journal,
and B's rename fails `ENOENT`.

**This design fixes that symptom, but does not fix that defect, and rhn5 must not
be closed as a duplicate.** Two writers can never be inside the staging window
simultaneously once the section of section 2 exists, so the collision becomes
unreachable on a locking filesystem. It remains reachable in exactly the case
this design cannot cover: `ownership.lock-unavailable` degradation (NFS). A
unique, journal-recorded staged suffix is a two-line change with no schema
impact and closes the hole in the degraded mode.

Two constraints rhn5 must respect, which fall out of this design:

- The staged path must remain **recorded in the journal** (`Journal.StagedPath`),
  because `removeStaged` (`journal.go:525`) and both recovery paths locate it by
  the recorded name. A randomly named staging file that is not journalled would
  leak on every crash.
- rhn5 should land **after** B1–B4 so its test can assert the ENOENT is gone in
  the degraded (lock-disabled) mode specifically, using B1's `WithLockTimeout(0)`
  / disabled-lock seam, rather than by racing.

`agent-marketplace-y1id`'s `Mutation.ExpectPriorDigest` is the CAS proposal; it
is adopted here as B5 and should be merged into that bead rather than duplicated.

---

## Bead breakdown

Six children of `9gmb`, in dependency order. Each is independently committable,
independently testable, and sized for one focused session. The shape is
expand-then-migrate: B1 and B2 add a mechanism nothing depends on yet, B3 and B4
migrate the two writer families onto it, B5 and B6 harden.

### B1. Advisory per-root file lock primitive in `internal/ownership`

Pure addition. New `lock.go`, `filelock_unix.go`, `filelock_other.go`; new
`locks/<rootKey>.lock` path helper; `WithLockTimeout` and a test-only
`WithLockObserver` option; the four diagnostic codes in `codes.go` **and**
`codes.md`. **No existing call site changes and no behaviour changes.**

*Acceptance:* two `Store` values for one root — in one process and in two `exec`d
processes — never hold the lock simultaneously; the lock is released after
`SIGKILL` of the holder; `GOOS=windows go build ./...` succeeds and the stub
returns `ErrLockUnsupported`; `locks/` is not counted by `EverRecorded` and a
first run still reports `ErrStateUninitialized`; `TestDiagnosticCodes_Documented`
passes with the new codes.

### B2. Serialize `Store.Apply`, `Store.Recover`, and `Store.BackupOnly`

Wire B1 into the store: a reentrant per-`Store` session so an outer `Lock` makes
inner acquisitions no-ops; acquisition before `validateMutation` so a refusal
still writes nothing. Ships the **O_EXCL sentinel harness** described in the test
strategy, so the whole suite from here on proves serialization deterministically.

*Acceptance:* `Apply` from two processes on one root produces two sequential
generations *N*, *N+1* with the record matching the file at every observation
point; the sentinel harness fails deterministically if the section is removed;
every existing `internal/ownership` test — including all eight crash-injection
points — passes unchanged; a benchmark shows the uncontended acquire cost on the
`Apply` path (per the benchmarking policy).

### B3. Widen the sync critical section around `Recover` + the per-file loop

`internal/materialize/devin.go` `reconcileDevinAggregates`: session opens before
`store.Recover()` (`:439`), closes after the `rels` loop; dry run stays
lock-free; `ownership.lock-contended` / `lock-timeout` plumbed into the report as
per-root diagnostics with the rest of the root skipped and the run exiting
non-zero.

*Acceptance:* two concurrent `materialize.Run`s on one repo produce a record that
describes the file at 100 % over 200 iterations (the bead's 60/60 repro inverted);
no `materialize.devin.aggregate-write` error is produced by contention alone;
a timeout skips exactly one root and leaves its aggregates byte-identical;
`sync --check` and `status` create no file under `$XDG_STATE_HOME`.

### B4. Widen the repair critical section and un-skip the known-defect repro

`Adopt`, `RemoveOrphan`, `ReplaceConflict` each wrap
`requireCompleteGeneration` + re-derivation + `Apply`. Remove the `t.Skip` from
`TestAdversarialConcurrentConflictReplacesForgeOwnership`.

*Acceptance:* that test passes unskipped at `-count=50 -race`; the lock is
provably not held across a prompt (a test that asserts `doctor --repair`'s
interactive loop holds no lock while awaiting input); `--yes` still never removes
an orphan or replaces a conflict; a concurrent sync and repair on one root
serialize rather than one destroying the other's journal.

### B5. Compare-and-swap precondition on `Apply` (merges `agent-marketplace-y1id`)

`Mutation.ExpectPriorGeneration` and `Mutation.ExpectPriorDigest`, checked after
`Load` inside `Apply`, plus `ownership.generation-conflict` (Error). Populated by
both writer families from the values they read at the top of their section. This
is the portable backstop for `ownership.lock-unavailable` filesystems.

*Acceptance:* with locking force-disabled, a concurrent `Apply` fails loudly with
`generation-conflict` and writes nothing rather than committing a mismatched
record; with locking enabled the precondition never fires (asserted by a counter
in the observer); `y1id` is closed by this bead.

### B6. Multi-process concurrency suite and the documentation of the contract

The suite that proves *serialization* rather than a narrower window (see below),
an `e2e/` standalone test running two real binaries against one sandbox `HOME`,
and the prose: `internal/ownership/doc.go` gains a "Serialization" section,
`AGENTS.md` gains one row in Key Design Decisions, `codes.md` gains the
`ownership.lock-*` narrative paragraph next to the existing `ownership.*` one.

*Acceptance:* the multi-process test fails on a tree with B3/B4 reverted and
passes on the full tree, at `-count=20`, with no `t.Skip`, no sleep-based
synchronization, and no tolerance for a "usually" outcome.

---

## Test strategy

The reviewer's warning is the design constraint: **narrowing a window without a
lock only makes the repro flaky, which is worse than leaving it.** A test that
races two writers and asserts "no corruption" proves nothing, because it also
passes on a tree where the window merely shrank. Every test below is therefore
built to fail *deterministically* on an unserialized tree.

### 1. The O_EXCL sentinel — the primary instrument (ships in B2)

Inside the critical section, at entry, create
`$XDG_STATE_HOME/locks/<rootKey>.inflight` with `O_CREATE|O_EXCL`; remove it at
exit. In production this is compiled out; in tests it is installed via
`WithLockObserver`.

If two sections ever overlap by even one instruction, the second `O_EXCL` create
fails with `EEXIST` and the test fails — **immediately, loudly, and with no
dependence on scheduling luck**. This converts "prove an absence of interleaving"
(unprovable by sampling) into "prove a single positive event never occurs"
(provable by the kernel). It is the difference between measuring a window and
proving mutual exclusion, and it is what makes B3's 200-iteration run meaningful
instead of decorative.

A negative control is mandatory: a test that installs the observer with the lock
disabled (`WithLockTimeout(0)` on a pre-held lock, or the `!unix` stub path) and
asserts the sentinel **does** collide. Without the negative control the
instrument could be silently broken and every test would pass vacuously.

### 2. Barrier-synchronized racers, not sleeps

Every concurrent test uses an explicit rendezvous: N goroutines/processes block
on a barrier, are released together, and the assertion runs after all have
joined. No `time.Sleep`, no "give it 100 ms". Where a test needs a writer to be
*inside* the section when another arrives, it uses the existing `Crasher` seam
pattern — a hook that blocks at a named point until the test releases it — so
the interleaving is constructed rather than hoped for.

### 3. Multi-process, because the in-process test cannot see the bug (B6)

Two forms:

- **Re-exec the test binary.** A `TestMain` mode flag (`AM_LOCK_CHILD=1`) makes
  the test binary run one `Apply` against a root given by env, so the parent can
  spawn 8 real OS processes against one `XDG_STATE_HOME`. This is the only test
  that would catch a fix implemented as a process-global mutex.
- **Two real CLI invocations** in `e2e/` (standalone, not table-driven — it needs
  concurrent `RunAM` calls the `Steps` model does not express): two
  `agent-marketplace sync` processes against one sandbox `HOME`, asserting both
  exit 0, the record describes the file, and neither emitted
  `materialize.devin.aggregate-write`.

### 4. Invariant assertions, not corruption sampling

After every race, assert *properties* rather than *absence of a symptom*:

- generations are consecutive with no gaps and no reuse (`Record.Generation`
  observed at each commit via the observer);
- for every `id` in `Proof.OwnedIDs()`, `Classify(id, digestFromFile(id)) ==
  StatusOwnedClean` — the exact assertion the unskipped repro makes;
- `ReadJournal()` returns nil at the end of every run;
- no `.am-staged-*` file survives in the install root;
- a retained safety backup holds the pre-race bytes.

These hold for *any* interleaving a correct implementation could produce, so
they do not encode a particular schedule.

### 5. Failure-mode tests, deterministic by construction

- **Timeout:** the test holds the lock itself (its own fd), runs a sync with
  `WithLockTimeout(50ms)`, and asserts one `ownership.lock-timeout` error, a
  byte-identical aggregate, a byte-identical record, and a non-zero exit. No race
  at all — the contention is manufactured.
- **Holder death:** spawn a child that acquires and then `SIGKILL` it; the parent
  must acquire immediately. This is the whole of section 4's stale-lock story,
  tested in one function.
- **Lock unavailable:** inject the `ErrLockUnsupported` path and assert exactly
  today's behaviour plus one `ownership.lock-unavailable` warning — i.e. that
  degradation is a *documented* fallback, not an accident.

### 6. Performance guard

Per the benchmarking policy, `Apply` is on the `sync` latency path, so B2 lands
`BenchmarkApplyUncontended` with before/after `benchstat` output in the commit
message. An uncontended acquire is one `openat` (amortized: the fd is kept for
the session) plus one `flock` — expected to be lost in the noise of the existing
fsyncs, and the benchmark is what makes "expected" into "measured".

---

## Open questions for the implementer

1. Should `ownership.lock-contended` be Info (recommended) or Warning? Info keeps
   a normal save-hook-vs-human race out of the warning count that `--strict`
   keys off; Warning makes it visible without `-v`. Recommendation: Info, with
   the wait duration in the message.
2. Default timeout: 30 s is proposed on the reasoning in section 3. If the fleet
   has CI jobs that legitimately run two syncs back-to-back against one machine
   root, 30 s may be generous rather than safe; measure before lowering.
3. Whether `deinit` (which reaches the path via `materialize.Run` with an empty
   selection, `cmd/agent-marketplace/deinit.go:127`) should use a *longer*
   timeout, since it is the one writer a user runs expecting a clean teardown.
   Recommendation: no special case in the first cut.
