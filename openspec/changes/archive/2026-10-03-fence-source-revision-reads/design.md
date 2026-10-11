## Context

Revision Build ingests resources then reopens their primary/support filenames after separate path checks. A hard link appearing at the real hash phase is accepted; support read failures may become warnings or primary canonical fallbacks, producing a partial/different revision.

## Decisions

1. Reuse the captured source FS per build, established containment/root/link policy and captured-size stable payload reads, instead of importing unapproved old source-tree limits. Primary/support paths stay lexically rooted; bytes never come from a path-authoritative reopen.
2. Boundary unsafe/path/generation/growth errors are hard ErrCatalog-wrapped failures retaining typed causes, not ordinary malformed omission. Ingest's typed source refusal is exposed from Catalog without parsing diagnostic text and carried into revision Build even if valid siblings remain.
3. Collections still hash canonical IR; existing source schema/byte semantics and genuine optional primary absence remain. Do not change hook-support alias contracts or claim whole-mutable-tree atomicity.
4. An internal Input callback follows the existing deterministic consumer test-seam pattern at the hash phase; no exported CLI fault interface.

## Verification

Public Build post-ingest primary hard-link witness was red/green: old hash accepted a manifest, new hash retains ErrUnsafeFile and produces no payload. A real intrinsic-support positive build followed by post-ingest support hard-link also refuses without omission/partial revision. Established compatibility goldens/determinism/canonical collections and strict-link tests, Linux/macOS, selected count20 races, affected source-revision/catalog/pipeline short packages and scoped validate/lint/build/spec gates pass. All nine implementation entries including test/script/spec exclusions reviewed, no skips. Final integrated R2 and GitHub comments stay pending until delivery completes. Existing native50skills/supports disk/no-network/log-quiet Build benchmark2s ×5 arm64 median38.193→49.911ms (no significant latency difference p=.548),2230387→2623094B/op(+17.61%),21001→25445allocs(+21.16%),p=.008 for allocation metrics. No95% interval with n5, concurrent host load was variable, and this is validation cost—not a speedup claim.
