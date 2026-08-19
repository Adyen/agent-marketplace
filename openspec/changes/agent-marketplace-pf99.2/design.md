# Design: Canonical Marketplace artifacts and analyzers

## Dependencies
- `agent-marketplace-pf99.1` froze OpenAPI, finding codes, consumer config fields,
  and the canonical artifact tar.gz/collection manifest skeleton.

## Decisions
- The analyzer engine returns `Finding` values, not a safety verdict.
  `Report.Rejected` is computed from `Policy.OutcomeFor(code) == reject`.
- Public policy rejects hidden content, terminal control, unsafe bidi,
  dangerous URLs, executable assets, suspicious paths, and obfuscated
  instructions. It warns on tool grants and hook/MCP capabilities.
- Private policy downgrades the two hidden-content codes to `warn` via
  `Policy.Outcomes`.
- Shared content scanners operate on UTF-8 text samples extracted from the
  resource fields and `FileSet` files. Binary assets are only scanned by the
  asset scanner to avoid false positives.
- The artifact builder wraps analyzed resources in `CanonicalArtifact` tar.gz
  archives, then decodes the raw archive so `rawArchive` is populated and
  `ValidateArchive` succeeds.
- Collection manifests are wrapped as canonical artifacts; the artifact
  content digest is over the collection manifest JSON payload.
- `SourceRevisionManifest` follows the same digest-exclusion pattern as
  `CollectionManifest`: the `revision_digest` is computed over canonical JSON
  with the field cleared.

## Files
- `pkg/marketplace/analyze/{engine,analyzer,options,policy,findings,scanners,assets,extract}.go`
- `pkg/marketplace/analyze/{skill,command,subagent,hook,mcpserver,collection}.go`
- `pkg/marketplace/artifact/{builder,revision,capabilities,findings}.go`
- `pkg/harness-compilation/artifact/canonical.go` (extended fields/limits)
- Corresponding `*_test.go` and `*_bench_test.go` files.
