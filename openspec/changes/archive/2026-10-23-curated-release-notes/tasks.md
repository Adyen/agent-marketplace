## 1. Spec deltas

- [x] 1.1 Add the `spec-governance` requirement for release notes on change proposals
- [x] 1.2 Add the `release-notes` capability for generated release notes

## 2. Gate rule

- [x] 2.1 Parse the `## Release note` section in `tools/specaudit` and check proposals added by a pull request
- [x] 2.2 Bind the gate scenarios with `// spec:` annotations in tests
- [x] 2.3 Document the rule in `openspec/config.yaml` and the pull request template

## 3. Generator

- [x] 3.1 Add `specaudit release-notes` with `-o`, grouping, and pull request attribution
- [x] 3.2 Bind the release-notes scenarios with `// spec:` annotations in tests

## 4. Release wiring

- [x] 4.1 Render the notes in the release job and pass `body_path` and `previous_tag` to the release action
- [x] 4.2 Add `make release-notes` and test the workflow wiring

## 5. Archive

- [ ] 5.1 Run `openspec archive curated-release-notes --yes`
