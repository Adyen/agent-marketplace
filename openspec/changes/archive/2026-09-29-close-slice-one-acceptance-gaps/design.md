## Context

The slice-1 stack already separates authored configuration intent from deployed evidence and collects caches offline. One unused loader facade still repeats the deployed-lock loop, a temporary Marketplace instance identity changes alias validation, and the GC command lacks integrated evidence for two cases in the existing design.

## Goals / Non-Goals

**Goals:** Reduce duplicate decision paths while preserving existing configuration authority and safety checks; prove cold-cache and trusted-lock collection through command wiring; measure loader and GC changes against the stack baseline.

**Non-Goals:** Changes to command context guards, TUI custom Loaders reload, Workspace root integration, or decoder unification.

## Decisions

- Compare normalized endpoint aliases by exact Marketplace instance ID. The `bootstrap` exception supported only an in-memory placeholder already removed from acquisition; leaving it gives unrelated instances a shared alias.
- Keep the deployed-lock reader for single-file configuration loading. Delete only the duplicate multi-layer loop and route multi-layer tests through the existing intent/bind boundary. Unifying single-file and pipeline decoders would change a separate trust boundary.
- Reuse one isolated GC command fixture for organization configuration, cache roots, network-dial observation, and unchanged organization files. An empty cache must prove absence of removals and lock writes; the trusted-lock case must include a real orphan to rule out blanket no-op behavior.
- Preserve named benchmarks and compare ten samples at the stack base and this layer head. Report endpoint-walk filesystem I/O rather than attributing all measurements to CPU.

## Risks / Trade-offs

- Removing test-only loader calls may mask subtle evidence differences. Keep existing config/pipeline behavioral tests and validate the in-package helper against the strict pipeline entry point.
- Cache tests that omit an orphan can pass when collection does nothing. Require both retention and real deletion under a trusted lock.
- The final benchmark can show a GC slowdown. Report raw results and workload, without a speedup claim.
