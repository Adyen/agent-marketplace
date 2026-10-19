## 1. Track OpenSpec changes

- [x] 1.1 Remove `/openspec/changes/` from `.gitignore`; verify `git status --short openspec/changes/bootstrap-openspec-authority` lists the change files
- [x] 1.2 Delete `openspec/changes/agent-marketplace-pf99.{1,2,3,4}/`; verify `openspec list --json` lists only active changes of this effort

## 2. specaudit traceability (`specaudit trace`)

- [x] 2.1 Scaffold `tools/specaudit` (stdlib only, `tools/ci-plan` style, subcommand dispatch, non-zero exit with `specaudit:` prefixed errors); verify `go run ./tools/specaudit --help` and an unknown subcommand exits non-zero
- [x] 2.2 Implement scenario parsing and slug derivation for main specs and change deltas (ADDED/MODIFIED/REMOVED/RENAMED sections); verify with tests annotated `// spec: spec-governance/slug-derived-from-title` and `// spec: spec-governance/duplicate-slug-rejected`
- [x] 2.3 Implement known-scenario computation and annotation scanning over `*_test.go`, `*.spec.ts`, `*.test.ts` (skipping `node_modules`, `.git`, and `openspec/changes/archive`); verify with fixture-tree tests annotated for `unbound-scenario-fails`, `dangling-annotation-fails`, `scenario-added-in-an-active-change`, `scenario-removed-in-an-active-change`, and `fully-bound-specs-pass`

## 3. specaudit archive and gate checks

- [x] 3.1 Implement `specaudit archived`; verify with tests annotated `spec-governance/completed-unarchived-change-fails` and `spec-governance/in-flight-change-passes`
- [x] 3.2 Implement `specaudit gate --base --head` path classification (behavior-bearing roots; test, Markdown, `testdata/`, `web/tests/` exclusions; spec-file detection); verify with temp-repo tests annotated `behavior-change-without-delta-fails`, `behavior-change-with-delta-passes`, and `test-only-change-passes`
- [x] 3.3 Implement `Spec-Impact` trailer parsing (`—` or `-`, non-empty reason, reason echoed on pass); verify with temp-repo tests annotated `trailer-with-reason-passes` and `trailer-without-reason-fails`

## 4. Bring existing specs under traceability

- [x] 4.1 Annotate an existing proving test for each `resource-prerequisites` scenario, or add a characterization test against current behavior; verify `go run ./tools/specaudit trace` reports no unbound `resource-prerequisites` scenario and the annotated tests pass
- [x] 4.2 Do the same for every remaining `marketplace-protocols` scenario (starting from `pkg/marketplace/api/openapi_test.go`); for any scenario that cannot be proven, add a REMOVED delta to this change with its reason and record it in `design.md` D9; verify `specaudit trace` and `openspec validate bootstrap-openspec-authority --strict` pass
- [x] 4.3 Verify the full tree: `go run ./tools/specaudit trace` exits 0 and `go test ./tools/specaudit/...` passes

## 5. CI and Makefile

- [x] 5.1 Add `OPENSPEC_VERSION ?= 1.12.0` and a `spec-check` target (pinned `npx --yes @fission-ai/openspec@$(OPENSPEC_VERSION) validate --all --strict --no-interactive`, then `specaudit trace` and `specaudit archived`); verify `make spec-check` passes locally
- [x] 5.2 Add a `specs` job to `.github/workflows/ci.yml` that runs on every pull request and push, independent of `tools/ci-plan`: checkout with `fetch-depth: 0`, pinned `actions/setup-go` and `actions/setup-node` (same SHAs as existing jobs), `make spec-check`, and on `pull_request` `go run ./tools/specaudit gate --base "$BASE_SHA" --head "$HEAD_SHA"`; annotate a workflow test (as in `tools/ci-plan/workflow_test.go`) with `spec-governance/invalid-spec-fails-ci` and `spec-governance/cli-version-is-pinned` asserting the job exists, calls `make spec-check`, and the Makefile version is exact; verify the test passes
- [x] 5.3 Add `tools/specaudit` to any Go test or lint scope that enumerates tools (check `Makefile` `test`/`lint-common` and `tools/ci-plan` categories); verify `make test-fast` or the equivalent includes it

## 6. Project context and conventions

- [x] 6.1 Move the `CONTEXT.md` glossary into `openspec/config.yaml` `context:` and delete `CONTEXT.md`; verify `openspec instructions proposal --change bootstrap-openspec-authority --json | jq .context` returns the glossary and its size is under 51,200 bytes
- [x] 6.2 Add `rules:` for `specs`, `design`, and `tasks` per design D5 and D8; verify `openspec instructions specs --change bootstrap-openspec-authority --json | jq .rules` shows them
- [x] 6.3 Repoint every reference to `CONTEXT.md` (search the tree, excluding `openspec/changes/archive`) to `openspec/config.yaml`; verify a search for `CONTEXT.md` returns only historical archive hits

## 7. AGENTS.md rework

- [x] 7.1 Rewrite `AGENTS.md` per design D7 (orientation, OpenSpec workflow rule, build/test/verification commands, `CODING_STANDARDS.md` reference) and move all existing behavioral prose verbatim under `## Unharvested leads (non-normative)`; verify a diff shows no behavioral text lost and the top sections contain no behavioral statements
- [x] 7.2 Apply the same rework to `pkg/harness-compilation/AGENTS.md`; verify the same way
- [x] 7.3 Add the OpenSpec workflow rule to `CODING_STANDARDS.md` only as a pointer to `AGENTS.md` if it currently states a conflicting process; verify no document other than `AGENTS.md` and `openspec/config.yaml` describes the spec workflow

## 8. Harvest ledger

- [x] 8.1 Create the ledger change with `openspec new change harvest-legacy-docs`, set `skip_specs: true`, and write a proposal plus a `tasks.md` with one unchecked task per legacy source listed in design D10 (expanding `docs/plans/*`, `docs/adr/*`, `docs/harness-validation/*`, and `pkg/harness-compilation/docs/plans/*` to individual files, and adding one task per `AGENTS.md` appendix); verify `openspec validate harvest-legacy-docs --strict` passes and `specaudit archived` still passes

## 9. Finish

- [x] 9.1 Run `make spec-check`, `go test ./...`, and `make ci-lint`; verify all pass
- [x] 9.2 Announce the new CI gate and `Spec-Impact` trailer in the Atrium town square before merge; verify the message is posted
- [x] 9.3 Run `openspec archive bootstrap-openspec-authority --yes`; if `openspec/specs/embedded-harness-compilation/spec.md` is left without requirements, delete that directory; verify `openspec validate --all --strict` and `go run ./tools/specaudit trace` pass after archiving
