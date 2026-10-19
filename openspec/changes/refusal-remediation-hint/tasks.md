## 1. Tests

- [x] 1.1 Add failing tests for writable-layer, organization, and unchanged-diagnostic guidance in config, CLI command, and TUI tests, annotated with `// spec: consumer-config-format/<scenario-slug>`; verify they fail on the base for the missing guidance

## 2. Implementation

- [x] 2.1 Attach layer-aware remediation to version refusals and verify the config tests pass
- [x] 2.2 Show the guidance once in CLI command errors and the TUI diagnostics view and verify the command and TUI tests pass

## 3. Documentation and archive

- [ ] 3.1 Describe the guidance in `site/docs/how-to/migrate-configuration.md` and `site/docs/reference/diagnostics.md`; verify `make spec-check` passes
- [ ] 3.2 Run `openspec archive refusal-remediation-hint --yes`
