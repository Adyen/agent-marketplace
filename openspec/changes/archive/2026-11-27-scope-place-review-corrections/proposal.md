# Proposal

## Why

Preserve compatibility for Git registry inputs without weakening exact profile identity, refuse ambiguous profile inspection, and pin the existing all-admitted-root verification timing with a focused witness.

## What Changes

- Separate registry URL normalization from strict target-profile origin identity so only one trailing `.git` is removed for registry configuration and acquisition locks.
- Refuse a changed inspection manifest atomically at the existing profile coordinate and byte-digest address while preserving the original manifest for clients with the old lock.
- Specify and test the existing initial-load barrier across admitted Place work, including pass/fail recording and skipped unusable targets; no production timing changes are planned.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `acquisition-lock-codec`: preserve registry URL compatibility while retaining strict profile identity admission.
- `marketplace-publication`: make inspection manifests immutable at an existing profile byte-digest address.
- `target-profile-place`: pin the existing admitted-work verification barrier.

## Impact

Changes are limited to Git URL and acquisition-lock normalization, Marketplace profile publication, and the existing Place initial-load timing witness. The lock format and production verification timing remain unchanged.

## Release note

Registry URL normalization removes only one trailing `.git`, while profile inspection changes at an existing content-addressed address refuse atomically.
