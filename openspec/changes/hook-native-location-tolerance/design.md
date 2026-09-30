## Context

See proposal.md. Current clients compile selected hooks into marker-owned legacy fragments for Claude Code, Cursor, Factory Droid, and Windsurf. The ownership store already records entry identities and canonical value digests for Devin CLI and Codex native hooks. Its entry records do not bind a native hook to the source hook selected by a different client version. Main does not include the catalog-driven native compiler planned for PR #403, so the compatibility reader cannot safely infer a native entry from the old plugin-format output.

## Goals / non-goals

The released client must recognize native output without producing it. It must not infer ownership from a marker, a resource name, or command similarity, and must never delete a native or handwritten hook. PR #403 later owns native compilation, publication and migration.

## Decisions

### Shared source fingerprint with ownership proof

Add an optional versioned source-hook digest to hook entries in the existing ownership state. It follows the version-1 length-prefixed byte sequence in the spec: stable source identity, installed name, target and scope, slash-separated source path and raw definition bytes, then every other regular file below the definition directory in lexically sorted relative-path order with mode and bytes. The broad file set is deliberately conservative: changing an unrelated sibling can prevent tolerance but cannot create false proof. Hash raw source data rather than versioned IR JSON or the source-revision generator projection; PR #403 changes IR provenance without changing source bytes. Neither commands nor support-file content appear in state. Freeze independent vectors at `internal/materialize/testdata/hook-source-fingerprint-v1.json`; both this change and PR #403 must consume that exact file and must not derive expected digests from the implementation. Keep old entries readable with no digest, but they cannot satisfy this compatibility check. The future native writer in PR #403 must record exactly this digest beside the native entry's existing total identity and canonical value digest. It must not emit a marker inside the native file.

The older client's read path opens exactly the native file listed in the spec's target/scope table, parses its hook container strictly and validates its path and shape. Windsurf's repository native `.devin/hooks.json` is distinct from legacy `.windsurf/hooks.json`; its machine native `~/.codeium/windsurf/hooks.json` is the same file the legacy marker merge touches, so compatibility suppresses only a proven entry and preserves unproven siblings in place. It reads existing ownership proof for that exact file, classifies each current native rule with the existing hook/v2 identity and canonical value digest, then compares its recorded source-hook digest with the current selected source. Only a clean proven entry with matching source digest suppresses the corresponding legacy fragment. The read path never opens a write session or creates the ownership store. A missing digest or unproven native entry follows legacy behavior; malformed native data or corrupt state refuses the affected target before planning a legacy mutation.

Alternative: compare command strings or resource names. Rejected because a changed matcher, event, support file or prompt can execute differently with the same name/command. Alternative: ship PR #403 catalogs in the prerequisite to compute future native output. Rejected because it duplicates the native compiler before its own specification and cannot prove which native bytes are owned.

### Compatibility boundary

Apply the check before marker-owned legacy fragments enter the merge/prune plan, using a single read-only helper for the four target layouts and both scopes. Suppress only the entry with matching proof; keep unrelated selected hooks and handwritten content. A read-only `sync --check` uses the same decision without writing. Do not guess a native path from a broad root walk; list only the native files and members planned for PR #403.

## Risks / trade-offs

- A missing source file, malformed native envelope, corrupt ownership record, or path outside the authorized root cannot establish proof. Preserve native data and refuse or retain legacy behavior as specified; never classify it as owned.
- If PR #403 changes the fingerprint algorithm or does not emit its optional state field, mixed-version tolerance will not activate. Include one shared golden fixture consumed by the prerequisite and future native-writer tests, and require the released-client gate after native migration.
- Reading and hashing source bytes adds work on sync. Reuse already-verified source files and benchmark the added hot path; bound reads using existing source limits.

## Migration plan

Merge and release this read-only prerequisite first. PR #403 then emits the agreed source digest with each owned native hook entry and tests that the pinned released binary succeeds at `sync --locked` and `sync --check` after migration. A release without native writers preserves current behavior. Rollback to a pre-prerequisite client may recreate legacy hooks, so the prerequisite release is the minimum supported rollback target after migration.
