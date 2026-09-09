# Package-manager lockfile research: Go modules, npm, and uv

**Status:** Research note; no Agent Marketplace design decision

**Date:** 2026-08-31

## Scope and source policy

This note compares first-party semantics that may inform later lockfile work. It
covers declaration versus resolution, stored identity and integrity data,
install/sync and update behavior, CI enforcement, partial updates, stale or
missing files, local sources, merge ergonomics, serialization, and transitive
closure. It deliberately does not propose an Agent Marketplace format or
workflow.

Only official documentation and project source are cited. Version-specific npm
links target npm 11.19.1. uv source links are pinned to commit `262a50bb` so
implementation observations do not drift.

## Summary

| Property | Go modules | npm | uv |
|---|---|---|---|
| Declaration | `go.mod`: module requirements, replacements, exclusions; `require` versions are minima | `package.json`: dependency specs/ranges and source forms | `pyproject.toml`: standard requirements plus uv-specific source overrides |
| Resolution record | No lockfile: MVS recomputes the deterministic build list; `go.sum` authenticates content | `package-lock.json` records the exact installed tree | `uv.lock` records a universal, exact resolution graph |
| Integrity / immutable identity | Module path + canonical version; `go.sum` `h1:` hashes module content and/or `go.mod` | Package location + exact version/source; tarball URL or Git commit; SRI integrity for unpacked artifacts | Package name + version + source; exact Git commit; registry sdist/wheel URLs and hashes |
| Normal operation | Build/test are readonly with respect to needed `go.mod`/`go.sum` changes by default; `go get`, `go mod tidy`, or `-mod=mod` reconcile | `npm install` uses locked versions while they satisfy `package.json`, otherwise resolves and rewrites | Project commands automatically check/update the lock and sync; existing locks are preferences |
| Strict CI | `-mod=readonly` rejects required `go.mod` or `go.sum` writes; `go mod tidy -diff` checks canonical tidiness | `npm ci` requires a lock, rejects manifest mismatch, deletes `node_modules`, and never writes manifests/locks | `--locked` requires an existing up-to-date lock; `--frozen` requires a lock but deliberately skips freshness checking |
| Partial update | `go get module@version` changes a target but MVS may also move related requirements | `npm update <pkg>` targets named packages within constraints; tree topology and affected transitive entries may change | `uv lock --upgrade-package <pkg>` upgrades one package while retaining other locked versions where compatible |
| Transitive closure | Computed from distributed `go.mod` graphs; not saved as one exact-tree lock | Yes, including physical `node_modules` placement | Yes, across supported markers/platform forks, extras, and dependency groups |

## Go modules

### Declaration and resolution

`go.mod` is both the project's declaration and part of the distributed module
graph. A `require` directive states a **minimum** module version. Minimal Version
Selection (MVS) follows requirements and chooses the highest required version
of each module, yielding the exact build list. The Go reference is explicit
that this build list is **not saved in a lockfile**: MVS recomputes it for each
module-aware command, is deterministic, and does not move merely because a new
release appears ([Go Modules Reference: MVS](https://go.dev/ref/mod#minimal-version-selection)).

For modules declaring Go 1.17 or newer, `go.mod` also explicitly requires each
module that provides a transitively imported package, enabling graph pruning
and lazy loading. Those `// indirect` requirements still are graph inputs, not
a serialized installation tree or a copy of every dependency edge
([Go Modules Reference: module graph pruning](https://go.dev/ref/mod#module-graph-pruning)).

### Stored identity and hashes

The selected identity is module path plus canonical semantic version. An
unreleased VCS revision is normalized to a pseudo-version containing a
timestamp and revision suffix, rather than leaving a branch name in `go.mod`
([Go Modules Reference: versions](https://go.dev/ref/mod#versions)).

`go.sum` is an authentication ledger, not the resolution record. Each line
contains a module path, version, and `h1:` hash; a version ending in `/go.mod`
hashes only that module file, while the line without the suffix hashes the
module's file tree. A build may need one or both. The checksum database can
verify that downloaded bytes match globally recorded values
([Go Modules Reference: `go.sum`](https://go.dev/ref/mod#go-sum-files),
[authenticating modules](https://go.dev/ref/mod#authenticating-modules)).
`go.sum` can contain checksums beyond the current pruned graph for compatibility
with an earlier Go version, so its entries are not a clean transitive-closure
enumeration ([`go mod tidy` compatibility](https://go.dev/ref/mod#go-mod-tidy)).

### Normal, update, and strict behavior

Since Go 1.16, ordinary module-aware build commands act as
`-mod=readonly`: if imports require a `go.mod` change, or a needed checksum
would have to be added, they fail and suggest a repair. `-mod=mod` permits those
commands to resolve missing modules and update `go.mod` and `go.sum`; `go get`
and `go mod tidy` perform such updates intentionally
([Go Modules Reference: build commands](https://go.dev/ref/mod#build-commands),
[official readonly checksum test](https://go.dev/src/cmd/go/testdata/script/mod_sum_readonly.txt)).
A missing `go.sum` is therefore recoverable in an explicit maintenance flow but
is an error in readonly operation when a checksum is needed.

`go mod tidy` adds requirements and sums needed by all packages and tests in the
main module and removes unnecessary ones. `go mod tidy -diff` emits the changes
that would be made and exits nonzero instead of writing, providing a whole-file
CI consistency check ([Go Modules Reference: `go mod tidy`](https://go.dev/ref/mod#go-mod-tidy)).

`go get path@version` upgrades or downgrades a named module, but MVS applies the
new constraint to the whole graph. Related dependencies can move, and a
downgrade may force other downgrades or removals. Thus the command is targeted,
while the resulting edit is graph-consistent rather than guaranteed to touch
only one line ([Go Modules Reference: `go get`](https://go.dev/ref/mod#go-get),
[MVS upgrade and downgrade](https://go.dev/ref/mod#minimal-version-selection)).

### Local paths, merge shape, and serialization

A `replace` directive may map a module (optionally one version) to an absolute
or `./` / `../` directory. The directory must contain `go.mod`; no replacement
version is written for a filesystem target
([Go Modules Reference: `replace`](https://go.dev/ref/mod#go-mod-file-replace)).
Go cannot compute a `go.sum` entry for a directory replacement, so local bytes
remain outside checksum authentication
([official command test](https://go.dev/src/cmd/go/testdata/script/mod_get_update_unrelated_sum.txt)).

The files are line-oriented. `go.mod` is designed to be human-readable and
machine-writable, and `go mod edit -fmt` applies standard formatting
([Go Modules Reference: `go.mod`](https://go.dev/ref/mod#go-mod-file),
[`go mod edit`](https://go.dev/ref/mod#go-mod-edit)). `go.sum` rewriting parses
entries, preserves still-valid existing sums, and emits a tidy sorted form
([`WriteGoSum` source](https://go.dev/src/cmd/go/internal/modfetch/fetch.go)).
This gives ordinary changes a mostly line-based diff. The reviewed first-party
material specifies no lockfile merge driver; semantic reconciliation is done by
rerunning `go mod tidy` after declaration conflicts are resolved.

## npm

### Declaration and resolution

`package.json` is the declaration: dependency fields contain package names and
specifiers such as tags, exact versions, semver ranges, Git URLs, tarballs, or
local paths ([npm package manifest](https://docs.npmjs.com/cli/v11/configuring-npm/package-json/#dependencies),
[package spec](https://docs.npmjs.com/cli/v11/using-npm/package-spec/)).
`package-lock.json` is generated output describing the **exact physical tree**
that npm produced, so later installs can reproduce it despite newly published
intermediate versions. npm intends it to be committed and describes it as a
single representation of the complete package tree
([npm lockfile description](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#description)).

### Stored identity, topology, and hashes

In modern lockfiles, `packages` maps package locations (for example,
`node_modules/foo` and nested placements) to package records. The root record
also carries the declaration fields. Installed records can include:

- exact `version`;
- `resolved`, normally a registry tarball URL, a full Git URL with commit SHA,
  or a link target;
- `integrity`, a SHA-512 or SHA-1 Subresource Integrity value for the artifact
  unpacked at that location;
- dependency specs and dev/optional/peer classification; and
- `link: true` for a symlink, whose target is represented elsewhere in the same
  lock.

These fields make package **location/topology** part of locked identity, not
only name and version. As of npm 7, the file contains enough data to reconstruct
the full tree without reading installed packages' manifests
([npm lockfile `packages` format](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#packages)).
It therefore represents the transitive closure and npm's hoisting/deduplication
result, not merely a flat version set.

### Normal install, stale or missing lock, and updates

With both files present, `npm install` compares them. If locked versions satisfy
the ranges in `package.json`, it installs those exact versions. If they do not,
`package.json` remains authoritative: npm resolves acceptable versions and
updates the lock. A missing lock is generated by an install that modifies the
manifest or tree. `--package-lock-only` resolves/updates the lock without
modifying `node_modules`; `--package-lock=false` ignores and does not write it
([npm install: lockfile behavior](https://docs.npmjs.com/cli/v11/commands/npm-install/#how-npm-install-uses-package-lockjson)).
Older lockfile versions are accepted and augmented from `node_modules` or the
registry when npm needs newer-format data
([handling old lockfiles](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#handling-old-lockfiles)).

`npm update [<pkg>...]` updates all named packages to the newest versions
allowed by both the root and dependent constraints, and installs missing
packages. With no names it updates all packages in scope. By default it does
not rewrite direct dependency ranges in `package.json`; `--save` does
([npm update](https://docs.npmjs.com/cli/v11/commands/npm-update/#description)).
Although a name scopes the request, npm still writes one valid exact tree, so a
changed package's transitive packages and physical placements may also change.

### Frozen CI

`npm ci` is the separate frozen path. It requires `package-lock.json` (or, in
npm 11, `npm-shrinkwrap.json`), errors if it disagrees with `package.json`,
removes an existing `node_modules`, installs the entire project, and never
writes the declaration or lock. Tree-shaping flags used to create the lock,
such as `--legacy-peer-deps` or `--install-links`, must also be supplied to CI
([npm ci](https://docs.npmjs.com/cli/v11/commands/npm-ci/#description)). It does
not support adding or updating one dependency.

### Local paths, merge behavior, and serialization

A local folder spec names a directory containing `package.json`. For a folder
outside the project root, normal installation creates a symlink unless
`--install-links` requests package-like installation; local links are recorded
as `file:` sources and `link`/target package entries
([npm package folders](https://docs.npmjs.com/cli/v11/using-npm/package-spec/#folders),
[npm install folders](https://docs.npmjs.com/cli/v11/commands/npm-install/#description),
[lockfile `resolved` and `link`](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#packages)).
The lock records the path/link relationship, not an integrity digest of a live
linked directory.

npm has explicit conflict recovery: after resolving `package.json`, running
`npm install --package-lock-only` can parse lockfile conflict sides and emit a
merged dependency tree. This behavior was first documented for npm 5.7 and is
implemented in npm's Arborist parser via `parse-conflict-json`
([official lockfile conflict documentation](https://docs.npmjs.com/cli/v6/configuring-npm/package-locks/#resolving-lockfile-conflicts),
[npm/Arborist dependency update](https://github.com/npm/cli/pull/4141)).

Serialization is generated JSON with a versioned schema. npm deliberately
infers indentation and line endings from `package.json`, so output formatting is
stable relative to the project but is not one project-independent canonical
byte representation; the public format does not promise canonical key ordering
([npm lockfile description](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/#description)).
Location-keyed full-tree records make changes reviewable, but hoisting changes
can produce multi-entry diffs.

## uv

### Declaration and resolution

`pyproject.toml` declares broad requirements in standardized project,
optional-dependency, and dependency-group fields. `tool.uv.sources` adds
development-time index, Git, URL, path, workspace, editable, and marker-specific
source overrides
([uv dependency fields and sources](https://docs.astral.sh/uv/concepts/projects/dependencies/#dependency-sources)).
`uv.lock` is generated exact resolution output. It is human-readable TOML,
uv-specific, checked into version control, managed by uv, and explicitly not
intended for manual editing
([uv project lockfile](https://docs.astral.sh/uv/concepts/projects/layout/#the-lockfile)).

The resolution is universal: one lock covers all supported operating systems,
architectures, and Python versions. It can hold multiple versions or sources of
a package with markers selecting the applicable fork. Project dependencies,
extras, and dependency groups are resolved together, subject to declared
conflicts
([uv universal resolution](https://docs.astral.sh/uv/concepts/resolution/#universal-resolution),
[uv conflicting dependencies](https://docs.astral.sh/uv/concepts/resolution/#conflicting-dependencies)).

### Stored identity, artifacts, and closure

A package node is identified by name, exact version, and source. Dependencies
are edges to other package IDs, optionally guarded by markers; every dependency
ID is required to have a corresponding package entry in the same lock. The lock
therefore contains the transitive graph, including platform forks, rather than
only direct pins
([uv `Lock` source](https://github.com/astral-sh/uv/blob/262a50bb/crates/uv-resolver/src/lock/mod.rs#L255-L303)).

For registry packages, entries record the index source and candidate source and
wheel distributions, each with URL, artifact hash, size, and (when available)
upload time. This is a set of platform-compatible artifact identities, not one
hash for an abstract package version. The repository's own lock illustrates the
schema and SHA-256 wheel/sdist records
([official `uv.lock`](https://github.com/astral-sh/uv/blob/262a50bb/uv.lock)).
When an index advertises multiple algorithms, uv chooses one hash per locked
distribution; policy can require a specific algorithm
([uv index hash algorithm](https://docs.astral.sh/uv/concepts/indexes/#requiring-a-hash-algorithm)).
Git sources are resolved to a commit SHA, and an existing lock retains that SHA
even when the declaration names a moving branch. Credentials are not stored in
the lock
([uv locked Git updates](https://docs.astral.sh/uv/concepts/projects/sync/#upgrading-locked-package-versions),
[uv index authentication](https://docs.astral.sh/uv/concepts/indexes/#authentication)).

### Normal sync, stale or missing lock, and updates

Locking and syncing are automatic. `uv run` locks and syncs before execution;
commands that read the lock can update it first. Freshness compares the lock
with project metadata: adding a requirement or excluding a locked version makes
it stale, while merely publishing a newer package does not. If a changed range
still contains the locked version, the lock remains current
([uv automatic lock and freshness](https://docs.astral.sh/uv/concepts/projects/sync/#automatic-lock-and-sync)).
Without a lock, normal project operation lazily creates one.

An existing lock is a resolver preference: `uv lock` and `uv sync` retain
locked versions unless constraints make them invalid. `--upgrade` refreshes all;
`--upgrade-package <name>` refreshes one named package (optionally to an exact
version) while retaining other locked versions where the graph allows it. All
upgrades remain bounded by declared constraints. The same flags on `uv sync` or
`uv run` update both lock and environment
([uv upgrading locked versions](https://docs.astral.sh/uv/concepts/projects/sync/#upgrading-locked-package-versions)).

`uv sync` installs the selected subset of the lock and is **exact** by default:
it removes packages absent from that subset. `--inexact` preserves extras.
`uv run` is inexact by default. Extras and groups select subsets of the already
universal locked graph rather than producing separate locks
([uv sync behavior](https://docs.astral.sh/uv/concepts/projects/sync/#syncing-the-environment)).

### Locked and frozen CI

uv distinguishes two non-writing modes:

- `--locked` asserts that resolution would leave `uv.lock` unchanged. A missing
  or stale lock is an error. `uv lock --check` is the standalone equivalent.
- `--frozen` requires a lock and uses it without checking against current
  project metadata. Newly declared but unlocked dependencies may therefore be
  absent from the environment.

The official GitHub Actions example uses `uv sync --locked`, making freshness
validation the normal CI posture
([uv CLI `--locked` and `--frozen`](https://docs.astral.sh/uv/reference/cli/#uv-sync),
[uv GitHub Actions guide](https://docs.astral.sh/uv/guides/integration/github/#syncing-and-running)).

### Local paths, merge shape, and deterministic serialization

Path sources can be local wheels, source archives, or project directories;
directories may be editable, and workspace members are always editable. The
lock records a `directory`, `editable`, or `virtual` path source and the local
package's resolved metadata. A live directory is represented by path and
metadata rather than a content digest; uv's rebuild detection separately uses
metadata-file timestamps and configurable cache keys
([uv path and workspace sources](https://docs.astral.sh/uv/concepts/projects/dependencies/#path),
[uv caching local dependencies](https://docs.astral.sh/uv/concepts/cache/#dependency-caching)).

uv canonicalizes output in source: it sorts each package's normal, optional,
and group dependency edges, then sorts package records by package ID before
serialization
([uv lock ordering source](https://github.com/astral-sh/uv/blob/262a50bb/crates/uv-resolver/src/lock/mod.rs#L584-L646)).
The lock also has explicit `version` and backwards-compatible `revision` fields;
newer unsupported schema versions are rejected
([uv lockfile versioning](https://docs.astral.sh/uv/concepts/resolution/#lockfile-versioning)).
These rules give deterministic order and make targeted upgrades comparatively
localized, although a package's affected dependency closure can still change.

The reviewed first-party material provides no lockfile merge driver or
conflict-aware merge command. Its documented contract is instead that `uv.lock`
is generated and not manually edited; after resolving `pyproject.toml` and
removing the conflicted generated file, `uv lock` regenerates a graph-consistent
canonical file. A textual conflict can be large because the universal file
includes all marker forks and artifact lists, even though package blocks and
edges are canonically ordered.

## Descriptive contrasts

1. **Resolution versus authentication are separable.** Go demonstrates a model
   with no resolution lock at all: deterministic graph computation and a
   separate checksum ledger. npm and uv serialize exact resolutions and include
   artifact integrity alongside them.
2. **“Frozen” is not a uniform term.** npm's frozen path validates declaration
   agreement (`npm ci`). uv's `--locked` validates agreement, while its
   `--frozen` intentionally does not. Go's readonly mode refuses needed module
   and checksum writes but uses a recomputed build list.
3. **A partial-update request need not imply a one-record edit.** All three
   systems preserve global graph validity; npm additionally records physical
   placement, and uv additionally records universal forks and artifact sets.
4. **Local directories weaken immutable-byte identity in every system.** Go
   records only the replacement path, npm records a live link/path, and uv
   records path plus resolved metadata. None hashes the complete live directory
   into its normal lock/checksum record.
5. **Merge cost follows representation.** Go's line-oriented minima and checksum
   ledger are relatively flat. npm stores a full location tree but supplies
   conflict-aware regeneration. uv stores canonically sorted package blocks and
   supports narrow upgrades, but documents regeneration rather than semantic
   merge.
