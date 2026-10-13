## Implementation

- [x] Add public `Convert`/`Compile` regressions annotated with `// spec: harness-compilation/<slug>` for all four scenarios, and confirm the refusal and reservation regressions fail on the prior code.
- [x] Refuse marked Devin converted command skills that declare `permissions`, `agent`, `subagent`, or triggers other than `[user]` with `ingest.devin.command-security-unsupported`.
- [x] Reserve `agent` and `subagent` on Devin command compilation.
- [x] Register and document the new diagnostic; update the field matrix, changelog and registry-author reference.
- [x] Update the existing Devin ingestion and compilation unit tests to the new contract.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive keep-devin-converted-command-restrictions --yes`
