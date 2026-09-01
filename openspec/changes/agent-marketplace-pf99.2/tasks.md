# Tasks: agent-marketplace-pf99.2

## Analyzer core
- [x] `pkg/marketplace/analyze/options.go` — `Options`, `Limits`, `Source`, defaults.
- [x] `pkg/marketplace/analyze/policy.go` — `Policy`, `PublicPolicy()`,
  `PrivatePolicy()`, `OutcomeFor(code)`.
- [x] `pkg/marketplace/analyze/analyzer.go` — `Analyzer` interface, `FileSet`,
  `Report`, `baseAnalyze`, limits, UTF-8 validation.
- [x] `pkg/marketplace/analyze/findings.go` — `NewFinding`, source-range helpers,
  conversion to `hccartifact.CanonicalFinding`.
- [x] `pkg/marketplace/analyze/scanners.go` — shared content scanners.
- [x] `pkg/marketplace/analyze/assets.go` — executable/shebang detection and
  suspicious path scan of file names.
- [x] `pkg/marketplace/analyze/extract.go` — reflection-based resource text
  extraction including nested `Extra` maps.
- [x] `pkg/marketplace/analyze/engine.go` — `Engine`, built-in registration,
  `Analyze` dispatch.
- [x] Per-kind analyzers (`skill.go`, `command.go`, `subagent.go`, `hook.go`,
  `mcpserver.go`, `collection.go`).

## Artifact and revision
- [x] `pkg/marketplace/artifact/builder.go` — `BuildResourceArtifact`,
  `BuildCollectionManifest`, `BuildResult`.
- [x] `pkg/marketplace/artifact/revision.go` — `RegistryRevisionManifest`,
  deterministic encode/decode/verify.
- [x] `pkg/marketplace/artifact/capabilities.go` — `ExtractCapabilities`.
- [x] `pkg/marketplace/artifact/findings.go` — finding conversion.
- [x] `pkg/harness-compilation/artifact/canonical.go` — `AnalyzerVersion`,
  `PolicyOutcome`/`Analyzer`/`AnalyzerVersion` on `CanonicalFinding`, total
  size/file limits on `ArchiveLimits`.

## Tests and benchmarks
- [x] Unit, adversarial, round-trip, determinism, policy, and rejection tests
  for `pkg/marketplace/analyze` and `pkg/marketplace/artifact`.
- [x] Benchmarks for hot paths (`Engine.Analyze`, `BuildResourceArtifact`,
  scanners, capability extraction).

## Verification
- [x] `go test ./...`
- [x] `make lint`
- [x] `make build`

## OpenSpec
- [x] `openspec/changes/agent-marketplace-pf99.2/{.openspec.yaml,proposal.md,design.md,tasks.md}`
