## 1. Tests

- [x] 1.1 Update package, command, and E2E telemetry tests for interactive-only consent, annotated with `// spec: telemetry/<scenario-slug>`; prove the removed flag is rejected and noninteractive diagnostics stay local

## 2. Implementation

- [x] 2.1 Remove the root flag and the `accepted` parameter of `Telemetry.FinishDiagnostic`; make noninteractive diagnostic export and preview explain the interactive-terminal requirement

## 3. Documentation and archive

- [x] 3.1 Update the telemetry and CLI reference pages under `site/docs`; verify `make spec-check` passes
- [x] 3.2 Run `openspec archive remove-diagnostic-accept-sensitive-data --yes`
