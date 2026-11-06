# Tasks

## 1. Owned-aggregate guidance

- [ ] 1.1 Copy the catalogued hint into owned-aggregate diagnostics and verify with a focused `internal/materialize` test that a diagnostic built for `materialize.devin.aggregate-shape` carries that code's hint
- [ ] 1.2 Add the owned-aggregate guidance guard in `internal/remedy`, give every flagged code guidance, and verify the guard passes and fails when a scanned code lacks guidance

## 2. CLI rendering

- [ ] 2.1 Skip empty remediation in every warning and error print path in `cmd/agent-marketplace/warnings.go` and verify with a test for an issue without a hint
- [ ] 2.2 Add a command test running `sync` against a hand-written flat-entry `.devin/hooks.v1.json` and verify it prints the `materialize.devin.aggregate-shape` and `ownership.state-missing` guidance as remediation and `hint:` lines, with no blank remediation line and no generic `sync --check` fallback

## 3. Archive

- [ ] 3.1 Run `openspec archive owned-aggregate-diagnostic-hints --yes` and verify `make spec-check` passes
