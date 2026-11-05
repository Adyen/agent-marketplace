## Implementation

- [x] Add a command regression annotated `// spec: command-error-output/malformed-resource-details-cannot-control-the-terminal` and confirm it fails on the prior code.
- [x] Escape the malformed-resource name, path and error in `sync --verbose`.
- [x] Run the affected command tests, `make validate`, `make spec-check` and `make validate-e2e`; review the change adversarially.
- [x] `openspec archive escape-sync-verbose-malformed-detail --yes`
