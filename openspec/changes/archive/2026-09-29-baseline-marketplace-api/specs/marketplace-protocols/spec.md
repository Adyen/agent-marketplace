## REMOVED Requirements

### Requirement: OpenAPI contract

**Reason**: The capability is rebaselined as `marketplace-api`; OpenSpec cannot rename a capability, so the drift-corrected requirements are removed and their proven behaviors are re-added under the new capability in wire-observable wording without Go identifiers.
**Migration**: Every scenario is carried into `marketplace-api` under a new scenario identity; the two drift-corrected scenarios are retitled there. Test annotations move in the same pull request.

### Requirement: Go API kinds and wire types

**Reason**: The requirement names package paths, functions, and internal types, which the spec rules forbid. Its observable content — the capabilities document and the shared wire vocabulary of selectable resource kinds — is re-added in `marketplace-api` as "Capabilities document and wire vocabulary".
**Migration**: The "Selectable resource kinds" and "Bidirectional kind conversion" scenarios carry over unchanged in substance, proven by the same annotated tests with moved identities.

### Requirement: Analyzer finding codes

**Reason**: The requirement names package paths and functions; its observable content is the stable finding-code catalog and its Markdown documentation.
**Migration**: Re-added in `marketplace-api` as "Analyzer finding codes" with the same "Code catalog parity" scenario, proven by the same annotated tests.

### Requirement: Consumer config fields

**Reason**: The requirement describes the `marketplaces` field as belonging to a legacy schema version, which is migration history the spec rules forbid, and its scenarios name validating functions and error types.
**Migration**: Re-added in `marketplace-api` as "Consumer Marketplace configuration" with the same three scenarios in wire-observable wording, proven by the same annotated tests.

### Requirement: Canonical artifact format

**Reason**: The requirement names a package path and internal types and functions (`CanonicalArtifact`, `Encode`, `Decode`, `VerifyContentDigest`, `VerifyArchiveDigest`, `ValidateArchive`, `ArchiveLimits`); its observable content is the artifact wire format itself.
**Migration**: Re-added in `marketplace-api` as "Canonical artifact format"; all five scenarios carry over, with the drift-corrected archive-digest scenario retitled to match its verification-outcome content, proven by the same annotated tests.

### Requirement: Collection manifest format

**Reason**: The requirement names a package path and internal types and functions; its observable content is the collection-manifest wire format itself.
**Migration**: Re-added in `marketplace-api` as "Collection manifest format" with the same three scenarios, proven by the same annotated tests.
