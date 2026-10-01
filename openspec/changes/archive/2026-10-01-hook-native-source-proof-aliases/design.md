## Context

Native hook identity is event plus canonical rule digest. Several selected resources can therefore share one identity while their source-hook fingerprints differ by installed name, source identity, target, or scope. The existing compatibility reader checks one scalar resource/fingerprint pair and requires every matching recorded entry to remain clean.

## Goals / Non-Goals

**Goals:**

- Represent all contributing source-hook evidence without changing native hook values or ownership identities.
- Keep old scalar evidence readable and preserve complete current-byte proof, target/scope isolation, and read-only behavior.
- Validate the added evidence as typed, non-secret data and prove the actual sync paths.

**Non-Goals:**

- No source-revision schema or generator changes; supported versions remain v1 through v4.
- No ownership-state version change, native hook writer, location migration, new prerequisite pull request, or handwritten adoption.
- No change to source/mirror fingerprint algorithms, resource selection, trust, or MCP behavior.

## Decisions

1. Add `HookSourceEvidence` with `Resource string` and `SourceDigest string`, serialized as `resource` and `source_digest`. A hook `Entry` may carry `SourceEvidence []HookSourceEvidence` under `source_evidence,omitempty`. Keep `Resource` and `SourceDigest` scalar fields and the existing state version.
2. Add `Proof.HasHookSource(id EntryID, resource, digest string) bool`. It matches either the scalar pair or an additional exact pair on a recorded hook entry. It does not grant current-byte ownership; callers must still classify the entry against its present value digest.
3. Use the accessor inside the existing complete-proof loop. All recorded entries matching the selected resource and fingerprint must be present and owned-clean. A clean match cannot override a missing or drifted matching entry.
4. Reuse scalar resource-name and digest validation for every tuple. Reject tuple evidence on non-hook entries, empty or mistyped members, invalid fingerprints, and ambiguous modeled fields. Use the existing typed JSONC decoding seam for ownership-state loading rather than adding another parser. Preserve the existing refusal codes and do not include rejected values in errors.
5. This change only reads additional evidence. A future native writer aggregates contributing tuples and refreshes source evidence when source content changes, even if native rule bytes are identical. It must not adopt handwritten entries. Reader tests seed future-writer ownership state explicitly; production legacy writing does not add tuples.
6. Retain independently authored source/mirror fingerprint vectors. Exercise four tolerance targets in repository and machine scopes, plus command-level complete/mismatched/partial proofs through sync, locked sync, and check. Snapshot native, legacy, acquisition-lock, and ownership-state bytes and mtimes for no-write assertions. Validation/logging controls contain sentinel rejected values and verify their absence.

## Risks / Trade-offs

- Optional tuple metadata adds work proportional to recorded contributor count. Preserve the existing scalar fast path and measure the native satisfaction hot path with independently specified scalar, shared, and mismatch inputs. Do not impose an arbitrary contributor limit or weaken validation to improve timings.
- A native writer that omits a contributor tuple remains unproven for that contributor; the reader must retain legacy behavior rather than infer equivalence from handler bytes.
- Clients released before this reader adjustment understand only the scalar pair. Native multi-contributor writes remain gated on a tolerance release that includes this adjustment.
