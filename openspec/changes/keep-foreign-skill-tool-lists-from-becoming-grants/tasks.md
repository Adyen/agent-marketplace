## Implementation

- [x] Add public `Convert` regressions annotated `// spec: harness-compilation/<slug>` for both scenarios, and confirm the omission scenario fails on the prior code.
- [x] Add `skill.allowed-tools-omitted` to the diagnostic catalog.
- [x] Omit foreign skill tool lists in the Claude, Cursor and Gemini skill compilers.
- [x] Update affected goldens and tests, the field matrix and the changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [ ] `openspec archive keep-foreign-skill-tool-lists-from-becoming-grants --yes`
