## Why

Devin CLI v3000.11.92 added `PreCompact` and `PostCompact` command hooks that run before and after context compaction, with `manual` and `auto` trigger matchers. Our Devin hook catalog lists neither event, so a Claude-layout `PreCompact` hook fails sync with `hook.devin.event-unmappable`. The catalog also maps canonical `PostCompact` onto Devin's older `PostCompaction` event, which Devin still supports but which no longer matches the native vocabulary.

## What Changes

- Declare Devin's `PreCompact` and `PostCompact` command events, with the lifecycle matcher, in the Devin hook catalog. A canonical `PreCompact` or `PostCompact` hook now compiles to the matching native event instead of failing sync.
- Mark Devin's `PostCompaction` event `ingest_only`. It stays accepted on ingestion and in same-harness output, but target selection never chooses it for a hook from another harness.
- Validate hook catalogs so an `ingest_only` event must name a canonical event, and at most one non-ingest-only plain event may compile each canonical event. Target selection now skips `ingest_only` events, so two plain candidates can no longer be resolved silently by sort order.
- Update Devin vocabulary in diagnostic hints, the diagnostic catalog, the field matrix, the Devin ingest hint, the Devin native probe's unrun-probe text, and the generated hook-and-tool-names reference.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-resources`: Devin compaction hooks compile to Devin's native `PreCompact` and `PostCompact` events; Devin `PostCompaction` remains accepted for ingestion and same-harness output.

## Impact

Affects the Devin hook catalog, the hook catalog loader and event selection in `pkg/harness-compilation/hookpolicy`, Devin hook diagnostics, the Devin ingest hint, the generated hook reference, and their tests. Devin CLI releases before v3000.11.92 do not run `PreCompact` or `PostCompact` hooks, so Devin output for those hooks is only useful on v3000.11.92 and later. No change to the frozen legacy hook tables, frozen source-revision fixtures, or consumer configuration format.

## Release note

Breaking: Devin CLI `PostCompact` hooks are now written to Devin's `PostCompact` event instead of `PostCompaction`, and `PreCompact` hooks now install for Devin instead of failing sync. Devin CLI releases before v3000.11.92 run neither event, so upgrade Devin CLI to v3000.11.92 or later to keep compaction hooks running.
