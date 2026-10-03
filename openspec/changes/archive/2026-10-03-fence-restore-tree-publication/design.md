## Context

The actual extraction consumer captures a root descriptor, then asks the compressed reader for its first bytes. A controlled input reader retargets the root at that point. Existing directory/symbolic namespace mutations reopen the absolute root and silently create a foreign directory or link.

## Decisions

Verify named extraction-root object against capture before admitted directory/symbolic operations. Reuse strict Dir.EnsurePath for directories/parents and os.Root descriptor-relative RemoveAll/Symlink for existing symbolic namespace overlay. Existing archive path and symbolic-target validation remains. This closes root/ancestor path reopening, not generation-atomic recursive deletion or symbolic publication: those leaf cleanup cases remain admitted follow-ups. Do not widen budgets, add durability syncs or claim a whole extraction transaction. Rename/race checks cannot exclude uncooperative writers.

## Verification

Directory/symbolic consumer cases fail on parent with successful foreign mutations; guarded version returns typed generation conflict before foreign entries. Independent valid input really creates the selected directory or symbolic leaf under retained root. Linux/macOS full backup/count20 races, validate/lint/build and strict specs pass. All ten implementation entries reviewed including consumer fault test, benchmark/script/spec exclusions without skips. Original comments and existing deletion/target policies retained. Narrow symbolic namespace/local disk/no network2s×5 arm64 legacy226.438→322.016µs, no significant timing difference(p=.421);240→168B(-30%),3→9alloc(+200%),p=.008,n5/no95%interval. Parent capture and added named-root checks excluded; timings varied under load(legacy76–438µs/captured216–428µs). Not full restore performance/speedup. Combined E2E/configuration checkpoint started on frozen runtime before final docs/archive publication, not final R2 closure.
