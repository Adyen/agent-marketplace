## Context

Repair already serializes each aggregate mutation under its ownership lock and checks aggregate and ownership generations before publication. The CLI's policy check occurs before entering that mutation, leaving a gap during lock acquisition and staging. GC already holds the exclusive exact-cache namespace lease, but its configuration and deployed-lock reachability can become stale during collector preflight.

## Decisions

- Carry the repair policy validator from the preview into the existing ownership mutation precondition. Validate before preparing a generation and again at the existing final publication check. Reuse failed-precondition cleanup so refused publication preserves aggregate and ownership bytes and leaves no staged file, journal, or newly retained backup.
- Keep all interactive prompts outside ownership and exact-snapshot leases. A generation conflict stops repair before another target is prompted.
- Build GC read assertions from the original configuration intent and deployed evidence, including absence assertions for undeployed acquisition locks. Validate these assertions after both preflights and before collection. Do not bind an absent organization Marketplace lock or contact the Marketplace merely to collect cache objects.
- Preserve current Workspace APIs and network-independent GC rather than restoring the superseded command implementation.

## Verification

Use the existing command-process isolation and built-binary sandboxes. Prove refusal for policy changes during confirmation, between CLI validation and mutation, during ownership staging, and during GC preflight, with unchanged-byte and residue assertions plus successful controls. Cover adoption, replacement, and orphan removal across Devin and Codex. Record GC and doctor benchmark baselines and rerun them after implementation. Run focused packages, `make validate`, strict spec validation and tracing, the PR spec-impact gate, and the combined E2E/configuration-compatibility gate.

### Benchmark measurements

Measured on macOS arm64 with an Apple M4 Max, using `-benchtime 2s -count 5 -benchmem` and benchstat. These are filesystem-inclusive, no-network empty-cache command fixtures. The baseline precedes the publication-boundary changes on the rebased PR.

| Benchmark | Baseline median | Final median | Allocation change |
| --- | --- | --- | --- |
| Doctor | 13.55 ms | 11.30 ms | 2594 allocs/op unchanged; byte difference not significant |
| GC dry run | 11.36 ms | 10.96 ms | 1122 allocs/op unchanged; time and byte differences not significant |
| Real GC | 9.619 ms | 12.174 ms (+26.56%, +2.55 ms) | 1221 to 2159 allocs/op; 190.4 to 282.7 KiB/op (+48.51%) |

Real GC pays for captured-input validation before destructive collection. The safety checks remain in place. Doctor's apparent speedup is not an optimization claim. Five samples do not establish a 95% confidence interval; rerun with more samples when comparing noisy filesystem costs.
