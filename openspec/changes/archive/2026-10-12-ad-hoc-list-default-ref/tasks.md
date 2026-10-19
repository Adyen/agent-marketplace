## 1. Implementation

- [x] 1.1 In `runListFrom`, resolve the registry default branch via `cache.DefaultBranch` when the entry is not a local directory and `--ref` is empty, after the allowlist check and before loading catalogs.
- [x] 1.2 Add a CLI test whose bare remote advertises a non-`main` default branch and assert `list --from <url>` without `--ref` lists the resources, annotated `// spec: ad-hoc-registry-browsing/ad-hoc-git-listing-without-a-ref-uses-the-default-branch`.

## 2. Validation

- [x] 2.1 Run the focused `list` command tests, then `make validate` and `make spec-check`.
- [x] 2.2 Adversarially review the intended delta (precedence, local directories, blocked URLs, read-only behavior) and record the outcome before pushing.

## 3. Archive

- [x] 3.1 `openspec archive ad-hoc-list-default-ref --yes`

## Evidence

- The new CLI test fails on the unmodified code with `validate registry ref: registry ref is required`, the exact error the report shows, and passes with the fix.
- `list --from https://github.com/anthropics/skills.git` without `--ref` listed the remote's skills after the fix; `--ref`, web-URL, and local-directory cases keep their existing tests.
- Focused `TestListFrom*` command tests passed. `make validate` passed after rerunning three command-test scenarios that hit the documented 120s child-process deadline under host load; all three passed alone. `make ci-lint`, `make build`, `make spec-check`, and `make validate-e2e` passed.
- Adversarial review of the delta confirmed the allowlist check still precedes any network access, explicit and web-URL refs still win, local directory registries still ignore the ref, and `list --from` still writes no configuration, lock, or materialized resource.
