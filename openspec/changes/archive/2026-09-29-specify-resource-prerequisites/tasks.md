## 1. Parsing tests

- [x] 1.1 Add `requirementsFromExtra` table cases for missing and empty `kind`, alias spellings (`MCP_Servers`, `agents`, `Commands`), `kind: all`, bare-string shorthand, surrounding whitespace in shorthand and in map values, and a non-map, non-string item. Annotate `resource-prerequisites/structured-item-without-kind`, `kind-alias-spellings`, `kind-all-means-skill`, `shorthand-without-a-kind`, `surrounding-whitespace-is-trimmed`, `non-list-requires-value`, and `invalid-item-among-valid-items`. Verify with `make test TEST_PACKAGES=./internal/catalog TEST_FLAGS="-run TestRequirementsFromExtra -count=1"`.
- [x] 1.2 Add an `internal/catalog` loader test that ingests a Claude-layout command and subagent whose front matter declares `metadata.requires` and asserts the parsed prerequisites. Annotate `resource-prerequisites/command-and-subagent-prerequisites` and verify the test passes.

## 2. Resolution and reporting tests

- [x] 2.1 Add resolver tests: an MCP prerequisite with a `registry` hint satisfied by a different registry produces no warning, and an MCP prerequisite whose name is selected only as a skill produces one warning. Annotate `prerequisite-supplied-by-another-registry` and `same-name-with-another-kind`, then verify with `make test TEST_PACKAGES=./internal/resolver TEST_FLAGS="-run TestResolve -count=1"`.
- [x] 2.2 Annotate `TestWriteGroupedWarningsIncludesRequirementRemediation` with `remediation-with-hints` and `TestUserFacingWarningsRedactURLCredentials` with `registry-hint-credentials-are-redacted`. Verify that `go run ./tools/specaudit trace` binds both.
- [x] 2.3 Annotate `TestFilterInstallWarnings` with `unrelated-unmet-prerequisite-is-omitted` and `TestExpandInstalledCollectionResourcesKeepsMemberWarnings` with `collection-member-prerequisite-is-reported`. Verify that `go run ./tools/specaudit trace` binds both.
- [x] 2.4 Add a `cmd/agent-marketplace` unit test that passes unsorted unmet-prerequisite warnings to the status row builder and asserts `RESOURCE`/`REQUIRES` ordering. Annotate `rows-are-sorted` and verify it passes with `go test ./cmd/agent-marketplace -run '^<TestName>$' -count=1`, placing the test in the in-process test file list or opening it with the isolation guard, as `TestCommandIsolationGuard` requires.

## 3. End-to-end tests

- [x] 3.1 Add a `sync --strict` `MustPass` step with the warning in `OutContains` to `mcp/sync_reports_unmet_prerequisite`, and annotate the case with `strict-sync-does-not-promote-unmet-prerequisites` and `commands-succeed-with-unmet-prerequisites`.
- [x] 3.2 Add an E2E table case in which a selected skill declares only malformed `requires` items (unknown kind, blank name, `collection:` shorthand). Sync must pass, the skill must be materialized, and the output must not contain `but it is not selected`. Annotate it with `malformed-item-stays-silent`.
- [x] 3.3 Run `make validate-e2e` and verify that the new and changed prerequisite cases pass.

## 4. Documentation

- [x] 4.1 Write `site/docs/reference/resource-prerequisites.md` following the design's reference outline, and check every rule against the spec delta.
- [x] 4.2 Write `site/docs/how-to/author/declare-resource-prerequisites.md` following the design's how-to outline, with the expected `install` warning and `status` table output copied from a local run against a directory registry.
- [x] 4.3 Rename `declare-tags-and-prerequisites.md` to `declare-tags-and-licenses.md` and reduce its prerequisite step to a link. Update `reference/resource-metadata.md`, `reference/registry-layout.md`, `reference/cli.md`, `how-to/troubleshoot-sync-drift.md`, `glossary.md` (add **Prerequisite**), `index.md`, and `site/.vitepress/config.mts` (sidebar entries for both new pages). Verify that searching `site` for `declare-tags-and-prerequisites` returns no hits.
- [x] 4.4 Build the site (`pnpm --dir site build`, or `./node_modules/.bin/vitepress build` inside `site`) and verify that there are no dead-link errors. Run `go test ./cmd/agent-marketplace -run '^TestDocsContract' -count=1` to confirm that the docs contract still passes.

## 5. Verification and archive

- [x] 5.1 Run `make spec-check` and verify that strict validation and `specaudit trace` pass with every new scenario bound.
- [x] 5.2 Run `make validate` and verify that it passes.
- [x] 5.3 Run `openspec archive specify-resource-prerequisites --yes` and verify that `openspec/specs/resource-prerequisites/spec.md` contains the added requirements and that `make spec-check` still passes.
