## Context

Native grouped command prune and uncovered-file-copy cleanup are distinct consumers. Original marked command installation plus a selected command/skill sibling makes both paths concrete; each previously deletes a replacement handwritten file. Native skill/resource-group legacy regular-file cleanup also deletes after a marker check.

## Decisions

Use one shared native removal wrapper: regular leaves refresh a scoped digest, while stale symbolic leaves retain managed-cache link authority. Reuse captured output removal to compare full observed state/digest/named parent before descriptor-relative mutation. Carry real harness boundary through grouped/uncovered callers. Keep user-owned/malformed/unsafe markers unowned, preserve dry-run behavior, and report only successful removals. Directory discovery itself remains a separate admission problem. No new limits, transaction or exclusion of uncooperative writers.

## Verification

Actual native grouped/uncovered install plus selected valid sibling, deterministic replacement refusal and preserved original/replacement bytes; healthy independent grouped/uncovered controls remove owned stale command. Linux/macOS native grouped/uncovered/healthy controls and existing prune/Factory controls, full short materialize, count20 races, scoped validate/lint/build and strict specs pass. All ten implementation files reviewed including tests/benchmark/script/spec exclusions, no skips; all three callers, scope/ownership/failure and fixture reachability checked. Review moved legacy generation capture before marker classification and corrected premature Removed reporting. Original comments retained. Post-classification local-disk/no-network comparison2s×5 arm64 syscall median130.925→870.968µs;128→21620B;1→294alloc,p=.008n5/no95%interval. Baseline51–235µs variable; initial marker classification not included, not an end-to-end prune/speedup claim. Prior combined E2E/configuration checkpoint through #492 passed; final R2 integrated/comment audit remains pending.
