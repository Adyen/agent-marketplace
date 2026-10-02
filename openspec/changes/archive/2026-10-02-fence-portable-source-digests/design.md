## Context

portableSkillVirtualDigest runs during read-only file-mode materialization and originally followed source aliases using path-based Stat/ReadFile. The mutating source copy already uses captured directory and payload readers.

## Decisions

1. Reuse those existing readers instead of importing the old broad source-tree port and its unapproved new budgets. Open one authorized source root, enumerate descriptor-relatively and read contained regular singly-linked payload snapshots with captured-size growth/full state validation.
2. Keep current artifact/VCS exclusions, virtual digest format/order and file permissions. Preserve legitimate contained relative file aliases and optional source roots; refuse escapes/special/multiply linked payloads before hashing bytes.
3. Scope is virtual support source hashing; portable destination hashing/copy/prune and primary ingestion authority are subsequent layers. Read-only checking must create no output/cache/manifest state.

## Verification

The actual native file-mode dry-run witness accepted a foreign alias before correction and reported no error; it now refuses with materialize.write.error without modifying external content or creating output/state. A contained-alias executable support installation remains in sync under checking and preserves ownership metadata. Its 0700 source directory revealed a separate mismatch (red/green): the virtual tree must mirror the mutating copy's 0755 support directories, not hash source-directory permissions. Nested enumeration failures no longer disappear as optional absence; optional missing roots remain. Supported Linux/macOS, native and v2/v4 CLI, count-20 selected races, scoped validate/lint/build and strict specs pass. All ten implementation entries, including exclusions, were adversarially reviewed; final integrated R2 gate and GitHub/comment review are deliberately pending until the stack is delivered. Read-only native disk/no-network check benchmark 2s ×5 median 1.305ms → 1.657ms (+26.94%), 250631 → 256700 B/op (+2.42%), 2635 → 2719 allocs/op (+3.19%), p=.008; n5 supplies no 95% interval and this is validation cost, not a speedup claim.
