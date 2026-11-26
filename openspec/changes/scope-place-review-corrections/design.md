# Design

## Context

Git identity normalization serves both strict target-profile identity and registry configuration or lock inputs. Profile inspection manifests are addressed by resource coordinate and profile byte digest, while the current-assessment pointer is selected separately.

## Goals / Non-Goals

**Goals:**

- Keep registry URL compatibility while preserving strict target-profile identity admission.
- Reject conflicting inspection manifests at the existing coordinate and profile byte-digest address before file-managed publication uploads.
- Prove existing initial-load ordering across admitted Place work, including outcome recording and unusable-target skips.

**Non-Goals:**

- Add a SourceID-specific address scheme or permit a changed manifest at the existing address.
- Change lock formats or production initial-load timing.
- Change graph or occurrence APIs, ordinary resource upload ordering, or managed publication fencing.

## Decisions

- Separate registry URL canonicalization from strict profile identity. Registry configuration and acquisition-lock readers remove one terminal `.git` and accept a remaining `.git`; target-profile identity applies the same normalization then retains its existing refusal for an unstable final repository name. The v1 and v2 lock formats remain unchanged.
- Put the profile inspection immutability guard in the current-assessment pointer update. Both SQL backends compare the stored pointer's inspection manifest with the candidate manifest as part of that update. File-managed profile publication keeps the metadata transaction open while it checks the pointer and uploads; a conflict therefore occurs before the inspection blob upload. The ordinary upload-before-metadata path and managed-fence publication path remain unchanged.
- Reuse the existing Place initial-load command witness and its hosted fixture. Keep assertions for failed publication, completed pass/fail outcomes, and skipped unusable targets; do not change production timing code.

## Risks / Trade-offs

- [Risk] A registry URL ending in multiple `.git` suffixes has different acceptance by design for registry inputs and strict profile identities. → Keep both cases independently covered by their existing annotated witnesses.
