# Property-Based Testing with Rapid

**Date:** 2026-09-28
**Status:** Approved design; implementation not started
**Worktree branch:** `hegel`
**Design base:** `8149c845168db2ebe16b7bc87b797629a58bcec0`

## 1. Decisions and objective

Add structured property-based tests for invariants that example tables cannot cover combinatorially. A failure must shrink automatically to a small, readable counterexample.

- Adopt [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) `v1.3.0` as a **test-only** dependency.
- Cover four targets in this plan: **A** ownership classification, **B** lockfile codec, **C** resolver selection policy, and **F** harness compilation.
- Run properties with Rapid's default check counts inside the existing `make test-witnessed` CI job. Add no new workflow or scheduled deep run.
- Ignore Rapid failure files (`testdata/rapid/`). Promote a real counterexample to an explicit example regression test in the same change as its fix.
- Complement existing tests rather than replacing them. Golden byte tests, the conversion matrix, E2E, adversarial, race, and security-boundary tests remain authoritative.

This document records the approved plan only. It does not authorize code, dependency, or CI changes.

### Rejected alternatives

| Alternative | Reason not chosen |
|---|---|
| [Hegel](https://hegel.dev/) (`hegel.dev/go/hegel`) | Beta API with breaking changes allowed; loads a native Rust `libhegel` extracted into `~/.cache`, conflicting with test machine isolation; no prebuilt Intel macOS library |
| `testing/quick` | Frozen; no shrinking; weak structured generation |
| `github.com/leanovate/gopter` | Older pre-generics API; manual generator/shrinker ceremony; weaker automatic minimization |
| Native Go fuzzing only | Well suited to raw bytes, but cumbersome for internally consistent `Config`, `Document`, IR, and ownership values. It remains a possible separate track for byte-oriented parser boundaries |

### Out of scope

- JSONC patch-engine state machine and `LayerEditor` operation sequences. They were considered and deferred; they are candidates for a later plan and relate to Bead `agent-marketplace-x89j.3`.
- TUI rendering, Cobra/E2E flows, real-process locking and races, Git/network/database integration, and exact golden layouts.
- Marketplace-resolver and collection expansion paths of `Resolve`.
- Nightly or high-count CI runs.

## 2. Shared infrastructure (Phase 0)

### Dependency and license

Add the dependency with `go get pgregory.net/rapid@v1.3.0`. It was published on 2026-03-30, which satisfies the seven-day minimum age policy, and it depends only on the standard library.

Rapid is licensed **MPL-2.0**. Obtain explicit license approval for a test-only MPL-2.0 dependency before merging Phase 0. No later phase merges without it.

### Keep Rapid out of shipped binaries

Rapid registers `-rapid.*` flags in `init`. The production binary must therefore never link it.

Add a test in `cmd/agent-marketplace` that runs `go list -deps ./cmd/agent-marketplace` through the Go toolchain already used by tests and fails if `pgregory.net/rapid` appears. Include a non-vacuity control asserting that a known production dependency, such as `github.com/spf13/cobra`, does appear, so an empty or failed listing cannot pass.

Generators and properties live only in `*_test.go` files beside the code they test. No shared generator package is added, since each target generates unrelated types.

### Repository hygiene

Add `testdata/rapid/` to `.gitignore`; Rapid writes minimized failure files there. Do not commit failure files.

### Budget

- Use Rapid's built-in counts: 100 checks per `rapid.Check` by default, which Rapid itself divides by 5 under `go test -short` (`make test-fast`). Rapid has no per-test check count, so no repository helper is added. `RAPID_CHECKS` remains a local override for developers.
- Each target package may gain at most about 2 seconds under a default `go test` and about 0.5 seconds under `-short`. Measure before and after, and record the timings in the commit message.

### Coverage by construction

The random seed changes on every run, and `-short` runs only 20 checks. A property therefore must not rely on randomly reaching a rare case, and a counter asserting "this case occurred" would be flaky.

Instead, each property runs as `t.Run(caseName, rapid.MakeCheck(...))` subtests, one per required case class. Each subtest uses a generator constrained to always include that class, for example "zero proof" or "required and excluded". An unconstrained "mixed" subtest explores freely. Coverage of every named class is then guaranteed in every run, including `-short`.

Rapid replays any failure file found under `testdata/rapid/<TestName>/` before generating new cases. Ignored failure files therefore still reproduce locally until they are deleted.

### Standards update

Add a short "Property-based tests" subsection under `CODING_STANDARDS.md` › Tests covering:

- Use Rapid for structured invariants, and native fuzzing for raw byte inputs.
- Oracles must be independent. Expected results come from a model written in the test, never from the production helper under test.
- Every property needs non-vacuity evidence that the generator reached the cases it claims to cover. Provide it by construction through constrained per-case subtests, never through probabilistic "this case occurred" counters.
- Assert error identity with `errors.Is`/`errors.As` or stable codes.
- Rapid is test-only, guarded by the dependency test.
- Minimized failures are converted to example regressions, not committed failfiles.

## 3. Target designs

Each target adds one `*_property_test.go` file in the package under test, so generators can use unexported constructors where the existing tests already do.

### A. Ownership classification

**File:** `internal/ownership/reconcile_property_test.go`
**Under test:** `Classify`, `ExactAdoptionCandidates`, and `ValueConflictCandidates` in `internal/ownership/reconcile.go`

**Generator**
- Draw from a small identity universe (two or three kinds, containers, and keys) so owned, existing, and desired sets overlap frequently. Digests come from a small pool so equal and differing values both occur.
- Build a `Proof` of owned entries with recorded value digests. In about one draw in five, use the zero `Proof` (missing or corrupt state).
- Build `existing` and `desired` slices of `EntryValue` with unique IDs per slice, in generated order.

**Model (written in the test):** map an ID to owned digest, existing digest, and desired digest. Classify from those three facts alone.

**Properties**
1. `Clean`, `Drifted`, and `Unmanaged` are pairwise disjoint, and their union is exactly the existing IDs.
2. An ID is `Clean` iff it is owned with an equal digest, `Drifted` iff it is owned with a different digest, and `Unmanaged` otherwise.
3. `Stale` equals owned ∩ existing ∖ desired.
4. `Collisions` equals desired ∩ existing, restricted to unmanaged entries.
5. With the zero `Proof`, `Clean`, `Drifted`, and `Stale` are empty, and every existing ID is `Unmanaged`. This is the fail-closed contract.
6. Adoption candidates are the unmanaged existing IDs whose desired digest is equal. Conflict candidates are the unmanaged existing IDs whose desired digest differs. The two sets are disjoint, and every candidate's digests match the model.
7. Every returned list is sorted, and shuffling `existing` and `desired` leaves every result unchanged.

**Case subtests:** `zero-proof`, `drifted`, `stale`, `collision`, `adoption`, `conflict`, and `mixed`. Each constrained generator guarantees at least one entry of its class. Keep the existing example tests (for example, `TestClassifyWithoutProofOwnsNothing`).

### B. Lockfile codec

**File:** `internal/lockfile/codec_property_test.go`
**Under test:** `Normalize`, `Encode`, and `Decode` in `internal/lockfile/codec.go`

**Generator**
- Produce valid v1 and v2 `Document`s with a valid scope.
- Registries use distinct URLs that are already canonical HTTPS URLs from a fixed host/path pool, distinct refs, and valid SHA-1 or SHA-256 OIDs.
- Roots and resources use portable paths that are already clean, distinct (kind, name) pairs, valid `sha256:` digests, and support-file maps.
- v2 documents may carry `Marketplaces` records with bindings, resources, and selections.
- Generated values are already canonical, so the expected semantic content is the generated document modulo order. The oracle is a test-side set/multiset comparison and never calls `Normalize`.

**Properties**
1. **Round trip:** `Decode(Encode(d))` succeeds and is set-equivalent to `d`, meaning registries, roots, resources, support files, and Marketplace records are equal as sets.
2. **Canonical ordering:** registries in the encoded output are ordered by (URL, ref), roots by (path, source harness), and resources by (kind, name). The ordering is checked with test-side comparators.
3. **Fixed point:** `Encode(Decode(Encode(d)))` is byte-identical to `Encode(d)`.
4. **Permutation invariance:** shuffling any list or map insertion order produces byte-identical `Encode` output.
5. **Duplicates:** an exact duplicate record collapses to the same bytes as the original. A same-identity record with a differing field fails with `lock.identity-conflict`, asserted through `errors.As` on `*lockfile.Error` and its `Code`.

**Replaces:** once properties 4 and 5 cover them, delete the fixed-seed shuffle loops `TestRegistryInputPermutationCannotChangeBytes` (`golden_test.go`) and `TestMarketplacePermutationAndDedupStable` (`marketplace_test.go`). Golden byte tests stay.

**Case subtests:** `v1-git`, `v2-marketplace`, `multi-root`, `support-files`, and `mixed`. Property 5 additionally has `exact-duplicate` and `conflicting-duplicate` subtests that always inject one duplicate.

### C. Resolver selection policy

**File:** `internal/resolver/resolver_property_test.go`
**Under test:** `Resolve` in `internal/resolver/resolver.go`, configured with Git registries, per-kind selectors, and optional `LocalConfig`. No Marketplace resolver or collections.

**Generator**
- Generate one to four `config.RegistryEntry` values. URLs, refs, and paths come from small pools so same-URL, different-ref, and overlapping-path cases occur. Each entry also gets a layer `Source` and a `Required` flag.
- Selectors per kind (skill, command, MCP server) are include/exclude lists over a small name pool.
- Optionally generate a `config.LocalConfig` with local excludes.
- Assign catalog keys with the existing `setCatalogKeys` helper. For each key, generate a catalog of `catalog.Entry` values with a (kind, name) pair from the same pools.

**Properties**
1. **Provenance:** every resolved artifact's (kind, name) exists in the catalog under its own `CatalogKey`.
2. **Required immunity:** an artifact selected by a `Required` entry is present regardless of repository, user, or local excludes for that (kind, name).
3. **Kind scoping:** an exclude for kind K never removes an artifact of any other kind with the same name.
4. **Boundary scoping:** an exclude declared on one registry entry never removes an artifact contributed only by an entry with a different URL, ref, or path.
5. **Determinism:** repeated calls return identical artifacts and identical errors, compared by type and code. Shuffling each catalog slice also leaves them unchanged.
6. **Monotonic isolation:** adding a catalog entry whose name no selector matches leaves the result unchanged.

**Explicit exclusion:** Bead `agent-marketplace-3q6` has not yet decided whether a same-kind/name collision picks a winner or refuses sync. Properties must not assert which occurrence wins. Collision draws assert only property 5 and that the reported warning is a `*NameCollisionWarning` (via `errors.As`).

**Oracle risk:** the selection model must restate only the documented rules listed above, not reimplement the resolver. Properties 1, 3, 4, and 6 are relational and need no full model. Property 2 needs only knowledge of which entries are required and what they select.

**Case subtests:** `required-and-excluded`, `same-name-other-kind`, `other-ref-or-path`, `local-exclude`, `collision`, and `mixed`. Each constrained generator guarantees its situation.

### F. Harness compilation

**File:** `pkg/harness-compilation/compile_property_test.go`
**Under test:** `Compile` for every registered target, and the same-harness round trip through `Materialize` and `IngestBundle`

**Generator**
- Produce valid IR hooks, MCP servers, and skills, limited to fields representable in the canonical fixture shape `canonicalFixture` in `matrix_test.go`.
- Names are kebab-case. Hook events and matchers come from the supported vocabulary. MCP servers are stdio or remote with string maps for environment variables and headers. Skill bodies are Markdown text without frontmatter delimiters.

**Properties**
1. **Deterministic compile:** calling `Compile(r, target)` twice yields identical artifacts, paths, bytes, and diagnostic codes for every target in `targetmeta.ConversionTargets()`.
2. **Same-harness round trip:** for each harness whose matrix cell for that kind is not a drop cell, compile, then materialize into `t.TempDir()`, then re-ingest, then `normalizeResource`. The result must equal `normalizeResource(r)` except for the cell's declared `allowedLosses`, which are reused from the matrix YAML through `loadMatrixSpec` and `assertCanonicalEqual`.
3. **Order independence:** the Devin hook aggregate bytes are identical for any permutation of a generated hook set. This generalizes the permutation loop in `TestDevinBundleCollapsesDuplicateHookBehaviorTuples`.

**Coverage and budget:** property 2 runs one `rapid.MakeCheck` subtest per non-drop (kind, harness) cell, enumerated from the matrix YAML, so every cell is exercised by construction. Round trips touch disk, so property 2 is skipped under `-short`.

If the default run exceeds the package budget, restructure so that each check round-trips one generated bundle containing every kind for that harness. That keeps one subtest per harness instead of per cell, with the loss profile still applied per kind. Record the choice and timings in the Bead. Properties 1 and 3 are pure and use the default counts.

## 4. Phases and work packages

Create one Bead per phase. Phases 1–4 depend only on Phase 0 and may proceed in parallel.

| Phase | Work package | Depends on | Acceptance |
|---|---|---|---|
| 0 | Rapid infrastructure and guardrails | none | MPL-2.0 approval recorded; dependency added; dependency-leak guard with a non-vacuity control; `.gitignore` entry; `CODING_STANDARDS.md` subsection; `make validate-fast` passes |
| 1 | Ownership classification properties (A) | 0 | Properties 1–7 pass in every case subtest; example tests retained |
| 2 | Lockfile codec properties (B) | 0 | Properties 1–5 pass in every case subtest; the two fixed-seed shuffle tests are removed in the same change |
| 3 | Resolver selection-policy properties (C) | 0 | Properties 1–6 pass in every case subtest; no collision-winner assertion |
| 4 | Harness compilation properties (F) | 0 | Properties 1–3 pass within budget; every non-drop cell is exercised by construction |

### Required evidence for every property work package

- **Mutation proof.** Temporarily break the invariant each property protects. Examples: remove a `sort` in `Normalize`, ignore `Required` during exclusion, or make `Proof.Classify` report an unrecorded entry as owned. Confirm the property fails and record Rapid's minimized counterexample in the Bead. Revert the mutation, and never commit it.
- **Cost.** Record per-package `go test` wall time before and after, both default and `-short`, and keep it within the Phase 0 budget.
- **Verification.** `make validate-fast` and `make test-witnessed` pass.
- **Counterexamples found.** A genuine defect found by a property is fixed under the normal bug workflow: a failing example regression test first, then the fix.

## 5. Risks

| Risk | Mitigation |
|---|---|
| Model restates the implementation (a tautological oracle) | Keep models to documented rules; prefer relational properties; review oracles against `CODING_STANDARDS.md` › Tests |
| Generators skew away from interesting cases | Small value pools that force overlap; constrained per-case subtests guarantee each class in every run |
| Test-time growth | Per-package budget; Rapid's built-in `-short` reduction; disk round trips skipped under `-short` and restructurable per harness |
| Rapid leaking into the shipped binary | Dependency guard test |
| Collision semantics change after Bead `3q6` | Properties avoid winner selection, so the decision requires no property rewrite |
