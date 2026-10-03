## Context

readDetailSourceLimit and renderBundleFile duplicate path-based O_NOFOLLOW open/stat/read logic. This admits hardlinks and blocks on FIFO before fstat. Their existing 256KiB source and 2MiB/64-entry capture policies are established, not a new R2 budget decision.

## Decisions

Reuse the captured destination streaming callback from #483 in one shared preview reader. Capture the resource containing directory for source reads and explicit resource root for selected bundle reads. Preserve no-follow preview policy by refusing captured final links, stream at most the old limit+1 inside captured-size authority, and discard bytes on refusal. Keep archive-backed in-memory content and existing comments/rendering paths unchanged. Conditional cleanup retains errors. No whole-tree snapshot claim.

## Verification

The bounded source preview/hardlink witness fails on parent after a valid truncation control, and passes with typed ErrUnsafeFile plus unchanged source bytes/identity/mode/mtime. Selected native bundle preview has rendered positive/error controls; bounded child FIFO probes cover both consumers without timeout goroutine leaks. Native snapshot capture also proves exact positive bytes and typed hardlink refusal with empty payload/marker. Existing detail/capture/bundle/terminal contracts and full short TUI tests pass on Linux/macOS, as do selected count20 races, scoped validate/lint/build and strict specs. Pinned frozen TUI QA setup/check passes; all three process checks and all thirteen current real-terminal journeys pass. Original source comments and archive rendering stay unchanged. All ten implementation entries reviewed including test/script/spec exclusions with no skips; capture scope, opened-state/prefix limits, no-follow policy, cleanup and byte/error disposition verified. Existing aggregate capture disk benchmark2s ×5 arm64 includes local source reads/no network; median754.012→2027.793µs(+168.93%),7211012→7257031B/op(+0.64%),304→880alloc(+189.47%),p=.008 n5/no95%interval. Validation cost, not speedup. Final integrated R2 and GitHub comments remain deferred until delivery.
