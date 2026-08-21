# Registry cache containment and publication

The registry cache (`internal/cache`) turns a `(url, ref)` pair into a directory
of checked-out registry content. Both halves of that pair come from
configuration and can therefore be attacker-influenced, and the resulting
directory is read concurrently by `sync`, `status`, the TUI and every
`materialize` run. This document records the invariants that make that safe and
the reasoning behind each one, because most of them are not visible from any
single function.

## Layout

```
~/.cache/agent-marketplace/                 cache root (the trusted root)
└── <sha256(url)[:16]>/                     per-registry-URL directory
    ├── <encoded-ref>/                      published entry: a git worktree
    ├── .<encoded-ref>.new-<random>/        scratch: a clone in progress
    └── .<encoded-ref>.old-<random>/        scratch: an entry being retired
```

The cache is disposable. Deleting it costs a re-clone and nothing else: no
ownership state, no configuration, and no aggregate-file bookkeeping lives here
(see `internal/ownership` for state that must survive).

## Invariant 1 — a ref is exactly one path component

`cacheRefComponent` encodes the ref; it never appears in a path as written. So
`../../outside`, `refs/heads/x`, and a 4 KiB ref are all single components, and
the encoding is rejected before it is used if the ref is not a plausible git ref
at all (`validateGitRef`).

The encoding is **base32, upper-case, unpadded**, not base64. base64's alphabet
contains both cases, so two distinct refs can encode to two components that
differ only in case — and APFS and HFS+ are case-insensitive by default, so those
two refs would share one cache entry and one would silently be served the other's
checkout. A single-case alphabet makes that impossible: distinct refs always
differ in some character, never merely in its case.

Refs whose encoding would exceed `maxCacheRefComponentLength` are replaced by
`@<sha256-hex>`. `@` is outside the base32 alphabet, so a digest component can
never collide with — or case-fold onto — an encoded one. The length budget is
well below `NAME_MAX` because the publication swap derives sibling names from the
component by adding a prefix and a random suffix.

## Invariant 2 — every filesystem operation is anchored below a resolved root

The cache *parent* (`~/.cache`) is resolved by path. A symlinked `HOME` and a
symlinked `~/.cache` are legitimate, common setups — relocated home directories,
a cache moved to another volume, `/Users` resolving through
`/System/Volumes/Data` on macOS — and refusing them would break ordinary
installs while buying nothing: a ref never gets to name any component of the
path used to *reach* the cache root.

Everything from the cache root downwards is reached only through `*os.Root`,
which refuses any name whose resolution leaves the root. So the cache root
itself, the per-URL directory, and every entry may not be a symlink to somewhere
outside the cache, and a `..` in any name fails rather than escaping.

## Invariant 3 — git is anchored by descriptor, not by path

This is the invariant that is easiest to lose. `os/exec` can only express a
child's working directory as a *string*, and the kernel re-resolves that string
when the child chdirs — after every check this package has made. Handing git a
path therefore leaves a window in which swapping one directory component for a
symlink redirects everything git writes to a location outside the cache. The
descriptor-based validation above does not close that window on its own; it only
closes it for operations this process performs itself.

So git is not given a path. `internal/cache/registry_gitexec_unix.go` re-execs
this binary with the validated directory inherited on file descriptor 3; the
re-exec `fchdir(2)`s to it, closes it, and becomes git. Every name git is then
given is relative to that descriptor, so git never resolves a cache path at all.
A descriptor names an inode, and an inode cannot be swapped.

Two consequences worth knowing:

- The hook lives in an `init()`, guarded by a private environment variable *and*
  by the inherited descriptor actually being a directory. It has to: the re-exec
  target is whichever binary linked the package — the release binary in
  production, a `go test` binary under test — so no single `main()` can be relied
  on to install it.
- It costs one extra process spawn per git command (measured at 5–10 ms;
  `BenchmarkGitInvocation` tracks it). End-to-end this is not measurable against
  the git operation itself: `BenchmarkCloneOrPullWarmLocalRepo` shows no
  significant `sec/op` change.

On platforms without `fchdir(2)` the working directory falls back to a path.
`PLATFORMS` in the Makefile is darwin and linux only; the fallback exists so
cross-platform analysis builds, not because the weaker guarantee ships.

## Invariant 4 — a ref resolves to a commit, or the operation fails

`git checkout <name>` silently reinterprets its argument as a **pathspec** when
no such ref exists. A registry pinned to a ref that does not exist, but that
happens to match a tracked path, therefore used to "succeed": the checkout was a
no-op, and the cache entry for that ref was published holding the *default
branch's* content.

`resolveCommit` resolves explicitly and in order — `refs/remotes/origin/<ref>`,
`refs/tags/<ref>`, then the ref as written — with `^{commit}` appended and
`--end-of-options` in front. `rev-parse` has no pathspec fallback, the result is
validated to be a full object name, and it is checked out with `--detach`, which
takes a commit and nothing else. An unresolvable ref fails and publishes nothing.

## Invariant 5 — a published entry is never mutated in place

A reader of a cache entry holds no lock; it walks a directory it was handed by
`CloneOrPull`. So an update that rewrote the worktree would let a reader see half
the old tree and half the new one.

- **Fetch** is done in the published entry, because it only writes objects and
  remote-tracking refs. A reader of the checked-out files cannot observe it.
- **A refresh whose ref still resolves to the checked-out commit writes nothing
  at all** — this is the common case, and it is the reason the hot path stays
  cheap.
- **A refresh that must change content is built out of place.** `git clone
  --local` into a scratch sibling hardlinks the object store, so no network
  transfer and no object copy is paid, and the hardlinks outlive the entry the
  swap retires. The requested commit is checked out there, and then two renames
  put it in place: the published entry is retired to a scratch name, the scratch
  entry takes its place, and only then is the retired directory unlinked. A
  reader resolving the path sees either tree, complete; a reader that already
  opened the old tree keeps reading it.
- **A cold clone is published with a single rename**, which is atomic.

A failed operation removes its scratch directory, and if it created the cache
root or per-URL directory in the same call it removes those too, so a failure
leaves neither a partial entry nor a cache skeleton.

## Invariant 6 — writers are serialized, and unserialized writers still converge

`CloneOrPull` takes an exclusive advisory lock for the duration of a mutation.
The lock is `flock(2)` **on the per-URL directory itself**, not on a lock file:
a lock file is state that must be cleaned up, and cleaning it up is exactly what
makes lock files racy (unlinking a file another process holds open silently
splits the lock). `flock` is owned by the open file description, so the kernel
releases it when the holder's descriptor closes — including on a crash — which is
why there is no timeout and no stale-lock heuristic.

Because the lock is per open file description, it serializes goroutines in one
process as well as separate processes. That matters: `catalog` fetches registries
concurrently.

Serialization is not the only line of defence, because a filesystem may refuse
advisory locks. Publication is therefore **convergent**: a rename onto an entry
another writer published first fails with `ENOTEMPTY`, and that is treated as
success. Both writers cloned the same ref of the same registry, so the winner's
entry is equivalent; the loser discards its scratch directory. A degraded run
says so in a warning rather than silently dropping the guarantee.

While the lock is held, any scratch directory present must be abandoned — the
lock is what proves no other writer owns one — so it is swept. That is how a
process killed mid-publication gets cleaned up.

## What is deliberately not guaranteed

- **Cache entries left by an older encoding are not migrated or removed.** They
  are inert: nothing resolves to them, and `agent-marketplace gc` removes a whole
  per-URL directory once no configuration references its URL.
- **Content-level trust.** Containment says a registry cannot write outside its
  cache entry. It says nothing about whether the registry's *content* should be
  installed; that is the selection and trust machinery's job.
