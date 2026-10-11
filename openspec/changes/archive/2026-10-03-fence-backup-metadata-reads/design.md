## Context

The set loader is a shared restore/list/rotation authority reader. Actual two-partition CreateCodexSet output is replaced by a manifest alias; the old reader imports the external matching metadata and performs restore. Regular manifest control restores the real archived content.

## Decisions

Capture backup directory once. fs.Glob over captured os.Root filesystem enumerates manifest names; archive-presence queries stay below it. Each manifest uses shared strict Destination captures with final-link refusal and captured-size regular/single-link/nonblocking state/named checks. Keep optional genuine missing directory and invalid JSON filtering, without treating shape/read failures as valid claims. No path-based manifest payload read remains. Archive payload admission/writing, extraction, manifest publication/rotation deletion and ownership evidence consumers remain separate, with no arbitrary new budgets or transactional archive promise.

## Verification

Actual two-partition backup + current installed skill, external manifest alias parent restore succeeds/red; guarded loader returns typed unsafe-path without changing current/foreign bytes/green. Restore with the original regular manifest independently changes current bytes to archived generation. Linux/macOS full backup package and count20 real restore-refusal/control races pass, with validate/lint/build and strict specs. Review all nine implementation entries including restore test/benchmark/script/spec exclusions without skips; shared restore/list/rotation call reachability, shape/refusal/absence/JSON filtering and close/descriptors checked. Original comments retained. One complete two-partition metadata-only/local disk/no network loader2s×5 arm64 baseline77.791→627.537µs(+706.70%),9264→15662B(+69.06%),85→167alloc(+96.47%),p=.008n5/no95%interval; after516–1087µs varied under build/lint load. Includes FDglob/presence refinement; not archive lifecycle/speedup. A concurrent lint-lock failure was rerun successfully with no gate/policy changes.
