## 1. Regression Tests

- [ ] 1.1 Add annotated regression tests for the five added scenarios and the modified ignored-mouse-events scenario; verify each scenario has its `// spec: tui-mouse-navigation/<scenario-slug>` witness, prove the focused selection tests fail at `ab521288c0b83ada9837acb81aa132baf617812e`, then pass on the implementation, and run `go run ./tools/specaudit trace`.

## 2. Right-Pane Selection

- [ ] 2.1 Implement pane-bounded, Unicode-safe drag selection, visible highlighting, copy-on-release, and selection invalidation for the right-hand details and file-preview panes; verify the focused selection, input, rendering, and existing wheel regressions pass without changing wheel routing or resource-list frame pacing.

## 3. Terminal and Performance Evidence

- [ ] 3.1 Add an encoded terminal QA journey for wheel navigation followed by right-pane drag and release; verify the highlight, exact OSC52 clipboard payload, and absence of clipboard writes for ignored gestures.
- [ ] 3.2 Capture comparable baseline and implementation input/render benchmarks and retain the results outside the worktree; verify selection mapping remains scoped to the visible selectable region without regressing input or render performance.

## 4. Guidance and Validation

- [ ] 4.1 Reconcile the published mouse-interaction guidance in `docs/tui-product-contract.md` and `site/docs/reference/tui.md`, updating `skills/agent-marketplace-development/SKILL.md` only if its relevant guidance conflicts; verify all updated claims match the specification and terminal journey.
- [ ] 4.2 Run the focused TUI regression command and final validation gates: `CGO_ENABLED=0 go test ./internal/tui -run 'Test.*(Selection|Mouse|FramePaced|HardExit|CtrlC)' -count=1`, `make validate`, `make spec-check`, `openspec validate --all --strict`, `make tui-qa-check`, and `make tui-qa`; retain the command output as evidence.

## 5. Archive

- [ ] 5.1 Run `openspec archive tui-preview-text-selection --yes` as the final task in the same pull request as implementation.
