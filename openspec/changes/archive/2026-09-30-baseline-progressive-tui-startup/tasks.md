## 1. Bind every scenario to its test

- [x] 1.1 Add one `// spec: tui-startup/<slug>` annotation line beside each existing passing test named in the design's scenario-to-test map (comment lines only, no test-logic change); verify `make spec-check` reports no dangling annotation and no unbound scenario

## 2. Verify the baseline

- [x] 2.1 Run `openspec validate baseline-progressive-tui-startup --strict` and `make spec-check`; then run `CGO_ENABLED=0 go test ./internal/tui ./internal/marketplace ./internal/pipeline -count=1` and verify the annotated tests pass unchanged

## 3. Archive

- [x] 3.1 Run `openspec archive baseline-progressive-tui-startup --yes` in the same pull request and verify `openspec/specs/tui-startup/spec.md` contains the capability, `openspec validate --all --strict` passes, and `make spec-check` passes with no `tui-startup` scenario or annotation left outside `openspec/changes/archive`
