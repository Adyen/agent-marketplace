## 1. Surface-neutral specification split

- [x] 1.1 Add the five `session-*` capability deltas and split the requirements in the design mapping; verify every neutral requirement retains at least one scenario and each moved-whole scenario retains its title.
- [x] 1.2 Remove the mixed `tui-interactions` multi-harness requirement and add its key-only residual without generic save/empty scenarios; change `tui-startup` to retain TUI-only residual requirements and remove only requirements moved whole; add TUI residuals for repository/personal-local migration isolation; verify no lower-layer or terminal-only assertion remains in a `session-*` scenario.
- [x] 1.3 Move annotations for whole-moved scenarios and bind every `session-*` scenario to a TUI-surface test under `internal/tui/` or `tools/tui-qa/`; add or annotate TUI tests for each neutral counterpart to a split scenario. Review the affected annotations found by `rg 'spec: tui-(startup|navigation|feedback|interactions|rendering)/'` in:
  - `internal/marketplace/cached_collection_test.go`
  - `internal/pipeline/marketplace_browsing_config_test.go`
  - `internal/tui/browsing_config_test.go`
  - `internal/tui/charm_v2_input_test.go`
  - `internal/tui/confirm_sync_test.go`
  - `internal/tui/fuzzy_rank_render_test.go`
  - `internal/tui/fuzzy_search_test.go`
  - `internal/tui/harness_test.go`
  - `internal/tui/huh_forms_behavior_test.go`
  - `internal/tui/marketplace_detail_test.go`
  - `internal/tui/marketplace_inspection_test.go`
  - `internal/tui/progress_findings_test.go`
  - `internal/tui/progress_test.go`
  - `internal/tui/progressive_stage_test.go`
  - `internal/tui/search_relevance_test.go`
  - `internal/tui/single_status_view_test.go`
  - `internal/tui/sync_reload_test.go`
  - `internal/tui/terminal_render_security_test.go`
  - `internal/tui/theme_native_test.go`
  - `internal/tui/toggle_flow_test.go`
  - `internal/tui/tree_test.go`
- [x] 1.4 Add annotated TUI tests for user-only legacy migration and hostile text inertness; verify declined migration does not write the user root and TUI residual tests cover repository/personal-local roots.

## 2. Shared interactive-session core

- [x] 2.1 Extract staged snapshot state, progress observations, generation isolation, mutation gating, discovery, and guarded actions behind a UI-agnostic event stream and command API; verify the core package has no Bubble Tea types.
- [x] 2.2 Move runtime lock/lifecycle reconciliation and `init` Harness-target detection/first-configuration logic out of package main; verify TUI startup and initialization preserve their existing outcomes.
- [x] 2.3 Adapt the TUI to the shared core while retaining terminal view state and input mechanics; verify existing focused TUI tests and annotations pass without user-visible changes.

## 3. Verification and finalization

- [ ] 3.1 Run `make spec-check` with `interactive-session-core` as the only active change in its layer PR and require zero trace findings: no unbound scenarios and no dangling annotations. Every `session-*` scenario MUST have a TUI-surface test annotation in this layer.
- [ ] 3.2 Run `make tui-qa` and affected TUI tests; verify established journeys and staged-startup/generation-isolation regressions pass.
- [ ] 3.3 Run `make validate` for the implementation layer and record the scoped result.
- [ ] 3.4 Run `openspec archive interactive-session-core --yes` in this layer's pull request.
