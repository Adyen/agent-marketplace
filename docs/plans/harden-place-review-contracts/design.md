# Design

## Context

See proposal.md for motivation and the two spec deltas for observable behavior. The design must preserve payload archive identity, existing legacy acquisition and publication behavior, and the established rule that recovery and read-only commands do not run host code.

## Goals / Non-Goals

**Goals:**

- Keep Place payload, dependency, inspection, and execution evidence independently bound to exact identities.
- Preserve currently accepted bounded YAML syntax and keep legacy acquisition behavior separate from Place-specific constraints.
- Make uncertainty in historical evidence, ownership, or execution binding a refusal that retains evidence.

**Non-Goals:**

- Change consumer configuration formats, ordinary non-Place revocation policy, legacy prerequisite publication, or archive-digest semantics.
- Infer missing historical Place evidence, automatically rewrite locks, or prescribe an unproven platform-specific execution mechanism.

## Decisions

1. **Keep prerequisite identity separate from payload identity.** The canonical Place dependency graph is immutable evidence in the exact Place lock, while the archive digest continues to identify payload bytes. A changed graph for identical bytes is publishable as distinct immutable Place evidence; it does not replace the graph attached to an earlier exact identity. If a historical lock lacks exact graph evidence, refuse rather than infer an empty graph or read a live graph. Folding `requires` into the payload archive digest would change payload identity, while reading the latest graph would make old locks mutable.

2. **Treat the existing inspection address as immutable evidence.** At the existing coordinate and profile byte-digest inspection address, a SourceID-only change that changes the manifest MUST refuse atomically. Preserve the original publication so a fresh client using the old lock retrieves the original manifest. The address remains bound to its manifest even when the payload byte digest is unchanged.

3. **Keep accepted parsing and discovery bounded without coupling their views.** Remove width-dependent pairwise mapping checks while preserving the existing byte limits, previously accepted keys, and all strict YAML validations; only keys already reserved by the applicable schema are reserved. Build independent legacy and Place discovery views from the same verified commit, applying aggregate acquisition limits across both. Preserve successful mixed views, required broken legacy-link refusal, and Place refusal when a legacy link overlaps selected Place paths. Unrelated dangling links outside both views do not affect acquisition.

4. **Isolate Place state and require provenance.** Store Place ownership and generations outside namespaces managed by historical writers and garbage collection. Independently prove that historical native writes and garbage collection preserve both ownership and generation bytes in pending and committed phases; do not require an old client to succeed while a journal is pending. A directory may be claimed, reused, or pruned as transaction-created only with recorded creation provenance. Require supported-filesystem evidence on Linux and macOS, not only a simulated recovery mismatch. Uncertainty retains the directory and evidence.

5. **Use an admitted-work barrier for initial-load checks.** Complete ownership commit and pruning for all approved Place work admitted to the invocation before running pending checks. Selected targets skipped as unusable are not admitted and do not block the barrier. Preserve each completed pass/fail result if another check fails. Retry a failed or pending check once on a later mutating sync only when its recorded projection is unchanged; recovery and read-only paths remain host-code-free.

6. **Fail closed only when execution bindings cannot be preserved.** Probes bind the validated working root and approved executable; checks additionally bind the exact validated subject. A pathname change alone does not require refusal when execution remains bound to the same validated object. Keep the existing preflight digest and drift refusals. For scripts and native executables, run only approved bytes or refuse before launch. Do not select a platform mechanism by assumption: implementation evidence must cover Linux and macOS, pathname replacement and in-place changes, and both executable forms.

7. **Reject scoped Place revocation overrides explicitly.** A scoped Place request with `--allow-revoked` is refused with an unsupported-option diagnostic naming the flag. Direct and collection-selected Place locks also refuse revoked revisions online and from cache. Keep ordinary non-Place policy unchanged and do not bypass assessment, availability, or archive checks.

## Risks / Trade-offs

- Risk: Historical Place locks may lack an exact prerequisite graph. Mitigation: Refuse when no original evidence exists; never adopt the current graph or assume empty.
- Risk: A SourceID-only inspection change may alter a manifest while leaving payload bytes unchanged. Mitigation: Refuse atomically at the existing coordinate and profile byte-digest inspection address and preserve the original manifest for clients using the old lock.
- Risk: A failed admitted root delays checks for already-published roots. Mitigation: Keep their results pending until all admitted work commits and prunes; skipped unusable targets do not block the barrier.
- Risk: Historical cleanup could mistake Place state for its own. Mitigation: Isolate ownership and generation storage and require independent historical native-write and garbage-collection witnesses for pending and committed state.
- Risk: Execution-object binding may not be available for every supported format or platform. Mitigation: Require supported Linux and macOS evidence for scripts and native executables; refuse before launch wherever the guarantee is not established. No security proof is claimed by this plan.

## Migration Plan

No consumer-configuration migration or automatic lock rewrite is introduced. Existing exact Place locks remain usable only when their original prerequisite graph can be established; otherwise delivery refuses. Keep historical publication, native sync, and garbage-collection behavior independently compatible.
