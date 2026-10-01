## ADDED Requirements

### Requirement: Exact Marketplace content access

Online code-mode skill access and declared MCP definition resolution SHALL refresh metadata for the exact catalog resource coordinate and resource revision before using a cached or fetched resource artifact. Validated metadata SHALL be cached for later offline access. A refreshed archive digest SHALL replace an obsolete cached archive digest without advancing the selected resource revision. Artifact bytes SHALL match the archive and content digests in that metadata as well as the artifact's internal digests. Online refresh failures, effective revocation, rejected assessments, and unavailable content SHALL refuse access without falling back to stale metadata or content and without starting a server. Revocation SHALL fail `resource.revoked`, rejection SHALL fail `resource.rejected`, and missing or unavailable content SHALL fail `resource.not-found`. Offline access SHALL use only validated cached metadata and verified artifacts and SHALL NOT perform network requests.

Marketplace skill support files and legacy asset files SHALL share a relative-path namespace. Skill loads SHALL list unique decoded relative paths in sorted order, returning at most 512 paths, and skill reads SHALL return the selected file's bytes only within the existing 4 MiB file limit and traversal/link restrictions. A path present in both file sets SHALL fail `resource.not-found` rather than select one arbitrarily.

#### Scenario: Online Marketplace access refreshes exact revision metadata

- **WHEN** online code-mode access finds cached metadata pointing to an obsolete or missing archive and the Marketplace returns a current archive for the same exact resource revision
- **THEN** access uses and verifies the current archive, caches the validated metadata, and preserves the selected resource revision

#### Scenario: Marketplace skill files include canonical support and legacy assets

- **WHEN** code loads or reads a Marketplace skill with canonical support files or legacy assets, including missing, oversized, traversal, and ambiguous duplicate paths
- **THEN** the load exposes the bounded sorted decoded paths and reads return their exact bytes, while invalid paths or sizes fail without returning bytes and duplicate paths fail `resource.not-found`

#### Scenario: Offline Marketplace access reuses verified exact content

- **WHEN** offline code-mode access requests an exact Marketplace resource revision from its cache, including absent metadata or absent or unverified artifact bytes
- **THEN** validated metadata and verified content are reused without network requests, and absent or unverified inputs fail `resource.not-found`

#### Scenario: Online Marketplace failures refuse stale fallback

- **WHEN** cached authorized content exists but online metadata refresh fails, reports effective revocation or rejection, reports unavailable content, or resolves artifact bytes that disagree with the expected digests
- **THEN** load, read, and declared server execution fail with a coded error, return no stale content, and start no server
