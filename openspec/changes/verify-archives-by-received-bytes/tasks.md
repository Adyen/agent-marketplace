## 1. Artifact core

- [x] 1.1 Add EncodeArchives, EqualIgnoringArchiveDigest, VerifyPlaceholderArchive, VerifyFinalAgainstPlaceholder, DerivePlaceholderArchive
- [x] 1.2 Share non-encoding validation between ValidateArchive and the placeholder path
- [x] 1.3 Placeholder fixtures for go1.26 and go1.27 and their tests

## 2. Storage and publication

- [x] 2.1 PlaceholderArchiveKey and placeholder upload validation in prepareUploads
- [x] 2.2 BuildResult.PlaceholderArchive and ingest ExtraBlobs
- [x] 2.3 Reuse of stored archives in the worker, with unit test
- [x] 2.4 Placeholder backfill at worker startup and at the end of reindex

## 3. API

- [x] 3.1 GET /api/v1/placeholder-archives/sha256/{hex} with shared availability gates
- [x] 3.2 Artifact route verifies against the placeholder when present
- [x] 3.3 OpenAPI document and generated web types (web/app/generated/api.d.ts)

## 4. Client

- [x] 4.1 Client placeholder fetch with 404-only fallback; mirror resolve verifies by received bytes
- [x] 4.2 Encoding-mismatch diagnostic with exact text, cache migration, and TUI remediation through resolver.Remediable
- [x] 4.3 Index-only resolution binds the registry revision manifest by hash and resource content, support, and file digests
- [x] 4.4 Client scenarios bound with // spec: annotations; docs updated

## 5. Verification

- [x] 5.1 Spec scenarios bound with // spec: annotations; make spec-check passes
- [ ] 5.2 Archive the change with `openspec archive verify-archives-by-received-bytes --yes` as the final commit of the pull request
