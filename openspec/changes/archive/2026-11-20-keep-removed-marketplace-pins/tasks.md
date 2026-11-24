## 1. Regression coverage

- [x] 1.1 Add failing pipeline and command tests with a fake Marketplace whose latest lookup returns 404 while the exact revision is served: one removed pin and one updatable pin; only removed pins; several removed pins; a removed collection pin; a required removed pin; a removed pin whose exact revision is also 404 with and without a verified cache entry; `--dry-run` and `--include-diff`.
- [x] 1.2 Add failing status coverage proving the DETAIL text, the warning, unchanged STATE, and unchanged exit status, plus no note in cached-only inspection.
- [x] 1.3 Add a TUI QA journey under `tools/tui-qa/tests/` that starts from a synced workspace whose Marketplace resource was removed upstream, presses the update key, and asserts the warning toast and the diagnostics entry; list it in `tools/tui-qa/README.md`. It must fail on the current base.

## 2. Implementation

- [x] 2.1 Classify a removed pin as kept-with-warning when its pinned revision is usable; keep the existing error otherwise.
- [x] 2.2 Emit the warning (with the remove hint, or the required-entry hint for required pins) on applying and preview updates, and exit 0 when no other error occurs.
- [x] 2.3 Note a 404 latest lookup in Marketplace status inspection without changing STATE.
- [x] 2.4 Surface the warning in the TUI update completion and diagnostics view.
- [x] 2.5 Update `site/docs/how-to/handle-renamed-or-removed-resources.md`.

## 3. Verification

- [x] 3.1 Run focused pipeline, marketplace, command and TUI tests, `make spec-check`, `make tui-qa`, and `make validate`.
