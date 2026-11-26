# Proposal

## Why

Place delivery and publication need stronger immutable evidence boundaries: identical payload bytes must not hide a changed dependency graph, and inspection or host execution must not silently switch to unapproved state. These contracts also need to preserve existing legacy acquisition behavior, revocation policy, and recovery guarantees.

## What Changes

- Bind Place prerequisite graphs separately from payload archive identity in exact Place locks, including historical-lock refusal when the original graph cannot be established.
- Keep Place payload and dependency-plan identity separate in publication, make inspection manifests immutable at a profile address, and refuse scoped Place revocation overrides without changing ordinary resource policy.
- Preserve bounded YAML syntax and strict validation while avoiding pairwise mapping scans; separate bounded legacy and Place views of a verified commit.
- Isolate Place generation and ownership state from historical writers and garbage collection, and require creation provenance before ownership, use, or pruning.
- Defer initial-load checks until all approved Place work admitted to the invocation has committed and pruned; skipped unusable targets do not block the barrier, and pass/fail evidence is preserved.
- Bind probes to their validated working root and executable, and checks additionally to their exact subject; refuse before launch only when those bindings cannot be guaranteed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `target-profile-place`: Specify immutable prerequisite evidence, Place revocation refusal, bounded parsing and acquisition views, isolated generation and ownership state, creation provenance, multi-root verification timing, and host execution bindings.
- `marketplace-publication`: Specify separate immutable Place payload and dependency-plan identities, plus immutable inspection-manifest identity for a profile address and atomic refusal of changed manifests.

## Impact

The change affects Place acquisition locks and cache reads, Marketplace Place inspection and publication, Git acquisition views, generation and ownership storage, multi-root sync ordering, and probe/check launch controls. Existing payload archive digests, legacy acquisition behavior, assessment and availability checks, recovery behavior, and ordinary revocation policy remain distinct contracts.

## Release note

Place locks and publication now preserve exact dependency and inspection evidence. Verification waits until all approved Place work admitted to the invocation is safely committed, and probes or checks refuse to launch if their approved bindings cannot be preserved.
