## 1. Failing regression tests

- [x] 1.1 Update `internal/catalog` tests for `kind: all`, mixed valid/invalid items and non-list `requires`; assert a stable `catalog.requires.invalid` diagnostic with source and index and no raw secret value. Change the `kind-all-means-skill` annotation to `kind-all-is-rejected`, and prove the targeted tests fail for the old behavior before touching production parsing.
- [x] 1.2 Add a CLI/E2E inspection case using a malformed but loadable registry resource and a valid sibling; assert `list --from` and `doctor` display the code, resource and source without leaking input and exit successfully. Make the targeted tests fail on the baseline first.
- [x] 1.3 Change the strict E2E case to expect failure for unmet prerequisites under `sync --strict` and `sync --strict --check` while normal sync succeeds and a satisfied strict control passes; update the renamed spec annotation, run it on the base, and observe failure due to the previous zero exit status.

## 2. Minimal behavior change

- [x] 2.1 Reject `all` only in prerequisite kind parsing and share catalog parsing between requirement extraction and diagnostic generation. Verify focused `internal/catalog` tests pass, including valid sibling, aliases and default skill.
- [x] 2.2 Propagate stable resource-located diagnostics through catalog load to `list --from` and `doctor` without turning the resource malformed or logging raw values; verify focused CLI/E2E tests pass for selected and unselected resources.
- [x] 2.3 Enforce unmet prerequisites under `sync --strict` and `sync --strict --check` without suppressing the warning, duplicating it, changing non-strict output or changing selection; verify focused command/E2E tests pass. Update any old strict test asserting the previous status.

## 3. Reconcile published claims

- [x] 3.1 Update `site/docs/reference/resource-prerequisites.md`, `site/docs/how-to/author/declare-resource-prerequisites.md`, and relevant CLI strict wording to reflect the new diagnostics, supported kinds and exit policy; verify link checker/site build and docs contract tests pass.
- [x] 3.2 Add `// spec:` witnesses for every changed or added child scenario and verify `make spec-check` reports zero unbound or dangling annotations. Do not edit the archived parent change.

## 4. Delivery

- [x] 4.1 Run `make validate`, `make validate-e2e`, and a site build with no failures; the parent runs the final clean-commit gates and independent reviews.
- [x] 4.2 Archive the child change with `openspec archive fix-resource-prerequisite-quirks --yes`, then verify the main spec and `make spec-check` before review and stacked publication.
