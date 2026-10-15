## Why

Registering OpenCode changed autodetection for existing registries. A manifest-less source with `.claude/` or `.codex/` resources and any `.opencode/` directory was detected as OpenCode, and so was a bare `skills/` or `.agents/skills` source beside one. OpenCode ingestion reads only `.opencode/`, so the previously detected resources disappeared from the catalog and sync pruned them without a warning. OpenCode creates `.opencode/package.json` by itself, so the trigger can appear unintentionally, and teammates on older releases kept the old layout.

## What Changes

- An OpenCode source marker is an `.opencode/` directory holding an OpenCode resource location or configuration file; dependency manifests alone are not.
- A source whose pre-OpenCode detection carries that harness's own workspace marker (`.claude`, `.codex`, `.factory`, `.cursor`, `.gemini`, `.windsurf`) keeps that layout. OpenCode still wins over shared skills locations (`.agents/skills`, a bare skills directory).
- A container root (bundle subfolders, no other root layout) stays a container beside an `.opencode/` folder, and container discovery skips `.opencode` as a harness folder.
- Whenever both layouts coexist, autodetection reports warning `ingest.autodetect.ambiguous` naming both and recommending `source_harness`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-opencode`: OpenCode detection preserves existing layouts and reports coexistence.

## Impact

Autodetection in `pkg/harness-compilation/ingest` and the OpenCode ingester's marker; catalog diagnostics. Explicit `source_harness`, plugin manifests, and Devin precedence are unchanged.
