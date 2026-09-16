# Canonical Marketplace artifacts and analyzers

## What
Implement the security analyzer engine and the artifact builder/revision
manifests that turn IR resources into deterministic Marketplace artifacts.

- `pkg/marketplace/analyze` — analyzer engine, policy, scanners, and per-kind
  analyzers for skills, commands, subagents, hooks, MCP servers, and collections.
- `pkg/marketplace/artifact` — `BuildResourceArtifact`, `BuildCollectionManifest`,
  `RegistryRevisionManifest`, capability extraction, and finding conversion.
- Extend `pkg/harness-compilation/artifact/canonical.go` with analyzer/version,
  policy outcome, and total-size/total-file archive limits.

## Why
pf99.1 froze the protocol surface; pf99.2 adds the runtime implementation
that produces, validates, and audits canonical artifacts. The analyzer treats
all indexed content as hostile and reports findings; policy decides whether an
artifact is rejected.

## Impact
- New Go packages and public APIs for analysis and artifact building.
- Deterministic tar.gz artifacts carry analyzer findings and capabilities.
- Collection manifests are wrapped as canonical artifacts; registry revision
  manifests provide a sorted, digest-covered index of a registry revision.
