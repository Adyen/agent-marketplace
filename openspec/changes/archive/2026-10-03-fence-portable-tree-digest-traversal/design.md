## Context

writePortableDigestPath classifies a directory, emits its header, then reopens its pathname for enumeration. A deterministic writer barrier reproduces a foreign directory retarget between those steps on #485, importing foreign payload bytes despite the separately guarded regular-file reader.

## Decisions

Capture the containing scope once; each entry is observed through a scoped native root. Directory descent uses strict captured children, descriptor enumeration and sorted names. Regular reads reuse the opened-file digest boundary with that same captured parent. Directory state and parent-relative names are revalidated after descendants; structural symlinks are read as text and their full metadata rechecked, never followed for payloads. Retain legacy byte encoding and special-object structural representation. Conditional cleanup retains failures. No whole mutable-tree atomicity, absolute budgets or publication/pruning closure claims.

## Verification

The changed-directory race is red/green after a valid tree digest; foreign bytes must never enter the digest, and external bytes/identity/mode/mtime remain unchanged. Legacy SHA ordering/structural-link controls and all existing portable/native updates/pruning/no-op/mode tests must continue passing. Linux/macOS portable controls and full short materialize tests, selected count20 races, scoped validate/lint/build and strict specs pass. All ten implementation entries reviewed including default-excluded tests/benchmark/script/spec files with no skips; captured directory/file descriptor lifetime, structural-link no-follow, full-state/named rechecks, ordering/encoding and refusal validity covered. Tree local-disk digest/no network,16 regular leaves,2s ×5 arm64 median3.106283→3.215702ms; no statistically significant latency difference(p=.056,n5). Bytes153760→98264(-36.09%),alloc1379→856(-37.93%),p=.008n5/no95%interval. Validation cost, not a lifecycle speedup claim. Final integrated R2/comment gates and output publication/removal/pruning remain queued.
