## 1. Hint lookup and catalog

- [ ] 1.1 Add the shared hint lookup (remediation, then code catalog, then command-aware fallback) and make existing coded error types expose their code
- [ ] 1.2 Write code-specific guidance for every in-scope consumer code, with organization-scope administrator variants, and move the TUI lock-view copy into the catalog
- [ ] 1.3 Add the guard test that fails on an in-scope code without guidance, and the rendering test over every entry; annotate `command-error-output/every-consumer-diagnostic-code-has-guidance`

## 2. Rendering

- [ ] 2.1 Print `hint:` lines for CLI command errors, including joined errors with deduplication and usage errors with a `--help` hint; keep JSON output, identity, and exit status unchanged
- [ ] 2.2 Print `hint:` lines under `doctor` configuration and organization errors
- [ ] 2.3 Use the catalog in the TUI errors screen before typed-cause and category copy
- [ ] 2.4 Add or annotate tests for every `command-error-output`, `consumer-config-format`, and `tui-error-recovery` scenario in this change

## 3. Message text

- [ ] 3.1 Name the authored file and line in consumer configuration validation errors and remove internal terms from their messages; update exact-message assertions
- [ ] 3.2 Prove that following the `v4.marketplace-lock-mismatch` hint clears the error

## 4. Documentation and archive

- [ ] 4.1 Add a what-to-do section to `site/docs/reference/diagnostics.md` consistent with the catalog, and reconcile the configuration and troubleshooting references
- [ ] 4.2 `openspec archive actionable-command-errors --yes`
