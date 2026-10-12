## 1. Implementation

- [ ] 1.1 In `runListFrom`, resolve the registry default branch via `cache.DefaultBranch` when the entry is not a local directory and `--ref` is empty, after the allowlist check and before loading catalogs.
- [ ] 1.2 Add a CLI test whose bare remote advertises a non-`main` default branch and assert `list --from <url>` without `--ref` lists the resources, annotated `// spec: ad-hoc-registry-browsing/ad-hoc-git-listing-without-a-ref-uses-the-default-branch`.

## 2. Validation

- [ ] 2.1 Run the focused `list` command tests, then `make validate` and `make spec-check`.
- [ ] 2.2 Adversarially review the intended delta (precedence, local directories, blocked URLs, read-only behavior) and record the outcome before pushing.

## 3. Archive

- [ ] 3.1 `openspec archive ad-hoc-list-default-ref --yes`
