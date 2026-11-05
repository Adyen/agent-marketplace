## Implementation

- [x] Add public `Convert` regressions annotated `// spec: harness-compilation/<slug>` for all four scenarios, and confirm they fail on the prior code.
- [x] Preserve unmodeled Gemini inline MCP server members and emit them without overriding modeled members.
- [x] Read Windsurf `working_directory` and refuse it on other targets during hook resolution.
- [x] Refuse Devin MCP servers with an unrepresentable argument.
- [x] Update affected unit tests, the diagnostic catalog, the field matrix and the changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive keep-restriction-bearing-native-fields --yes`
