# R5 R47: Repeated Git Root Requirements

## Scope

Start independently at main `ced6f887`, separate from open R35 PR #745.
Frozen R47/F116 concerns repeated source-revision builds during direct-Git
lock acquisition. Keep the frozen ledger/source index unchanged and add no
D1-D6 decision, quota, deadline or policy ceiling.

## Contract And Options

`reconcileLayerDocument` groups by canonical repository and ref within one
layer. `acquireLockedRegistry` resolves/acquires one exact snapshot, builds
every requested root, then deduplicates output by root path and resolved
source harness. Repeated requests therefore repeat ingestion, discovery,
hashing and archive work before their duplicate output is discarded.

- Keep those builds: preserves behavior but repeats work.
- Cache across operations or key only by resolved output: risks stale
  snapshots or conflating explicitly requested source policies.
- Skip exact repeated `(root path, requested source_harness)` inputs only
  within the current acquisition. Retain the separate existing resolved
  output identity and its first-occurrence behavior.

Measure the third option. Reuse `lockfile.GitRootRequirement` as the key;
normalize only empty paths to `"."`, exactly as before. Do not equate
auto-detection with an explicit hint, even when both resolve alike.
Successful builds alone populate the set. Repository/ref/snapshot/schema/
scope boundaries remain the invocation's existing boundaries, not a
process-global cache. Grouping, source selection and downstream snapshot
verification/publication remain unchanged.

## Evidence

Use the actual acquisition path with local bare Git origins and isolated
HOME/cache/state/config. Preserve exact registry/root/resource/support
inventory and compare actual CLI lock bytes, repeated no-ops and refusals.
Include implicit/explicit policy orders, duplicate aliases, distinct roots
and policies, canceled requests and invalid requirements.

Count existing structured build-debug records through the real builder for
a deterministic regression: duplicated requests must build once, while
distinct policies still build separately. No fake builder or production
test hook is needed.

Benchmark real warmed acquisition with local Git processes and disk I/O,
including ref resolution, snapshot validation and source builds. Exclude
fixture creation/seed sync and network. Retain identical five-sample
before/after data and small/distinct-policy controls. Run focused command,
source-revision/cache/lock/catalog/materializer tests, races, strict specs
and combined E2E. Review all intended files via host OCR delegation before
push; use justified `Spec-Impact: none` trailers for unchanged behavior.
