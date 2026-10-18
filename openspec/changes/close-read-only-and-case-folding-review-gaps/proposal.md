## Why

CodeRabbit posted three findings in review bodies on #723 and #732, outside the review threads, and they were not addressed before merge:

1. **Codex read-only subagents never reached their mapping.** The Codex native-loss guard refused every Codex sidecar `sandbox_mode` on other targets before compilation ran. So a Codex `sandbox_mode = "read-only"` subagent was suppressed on Claude, Cursor and Factory instead of mapping to their read-only modes, which the read-only requirement already counts it as.
2. **Case-only collisions used ASCII lowering.** Artifact-path collision detection used `strings.ToLower`, which keeps Greek `σ` and final `ς` distinct although Unicode case folding equates them. A case-insensitive volume would still fail the staged write.
3. **The fallback digest test derived its expected value from the helper under test.**

## What Changes

- The Codex guard lets a `read-only` sandbox through on targets with a read-only mode (Claude, Cursor, Factory), and keeps refusing every other mode and target.
- Artifact-path collision keys use Unicode simple case folding (`ir.CaseFoldKey`).
- The digest test compares against independently serialized legacy resources.

## Capabilities

### Modified Capabilities

- `harness-compilation`: read-only subagent restrictions are mapped or refused (Codex source); artifact paths that differ only in letter case collide (Unicode folding).

## Impact

`compile.codexNativeLossDiagnostics`, `resolveArtifactPathConflicts`, a new `ir.CaseFoldKey` helper, and tests.
