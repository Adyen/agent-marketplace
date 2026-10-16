## Implementation

- [x] Add public `Convert` regressions annotated `// spec: harness-compilation/<slug>` for both scenarios, and confirm they fail on the prior code.
- [x] Refuse duplicate Windsurf workflow and rule names within one workspace root with `ingest.windsurf.duplicate-identity`.
- [x] Reject repeated keys in the tolerant rule frontmatter parser.
- [x] Add the diagnostic to the catalog and update the field matrix and changelog.
- [x] Run affected packages, `make validate`, `make spec-check` and `make validate-e2e`; review every changed entry adversarially.
- [x] `openspec archive refuse-ambiguous-windsurf-and-rule-identities --yes`
