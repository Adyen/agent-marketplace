## 1. Reproduce and fix discovery

- [x] 1.1 Add a failing-first directory-source regression with a `// spec: directory-source-discovery/git-metadata-beside-valid-and-malformed-skills` binding; verify that a source containing `skills/.git`, a valid skill, and a malformed authored skill reports only the latter as malformed in CLI/TUI discovery.
- [x] 1.2 Exclude `skills/.git` before source-layout ingestion treats it as a skill; verify focused ingestion and consumer regression tests pass and explicitly selected roots remain loadable.

## 2. Verify and publish

- [x] 2.1 Run focused tests, `make validate`, and `make spec-check`; verify no false Git metadata errors or unexpected changes to materialized resources.
- [ ] 2.2 Run `openspec archive ignore-local-source-git-metadata --yes` as the final implementation commit in the same pull request and verify the main spec contains the scenario.
