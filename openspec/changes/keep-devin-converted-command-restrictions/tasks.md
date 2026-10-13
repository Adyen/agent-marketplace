## Implementation

- [ ] Add public `Convert`/`Compile` regressions annotated with `// spec: harness-compilation/<slug>` for all four scenarios, and confirm the refusal and reservation regressions fail on the prior code.
- [ ] Refuse marked Devin converted command skills that declare `permissions`, `agent`, `subagent`, or triggers other than `[user]` with `ingest.devin.command-security-unsupported`.
- [ ] Reserve `agent` and `subagent` on Devin command compilation.
- [ ] Register and document the new diagnostic; update the field matrix, changelog and registry-author reference.
- [ ] Update the existing Devin ingestion and compilation unit tests to the new contract.
- [ ] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [ ] `openspec archive keep-devin-converted-command-restrictions --yes`
