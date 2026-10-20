## 1. Tests

- [x] 1.1 Add a Go test that a TUI discovery install prepares with the lifecycle's dependency reconciler, and the TUI QA journeys `marketplace-discovery-install` and `marketplace-install-consent`, annotated with `// spec: tui-interactions/<scenario-slug>`; reproduce the lock bypass on the base in a real terminal

## 2. Implementation

- [x] 2.1 Add `InstallReconcile` to `tui.DependencyLifecycle`, pass it to discovery installs, and wire `reconcileCommandDependencies` in production; verify the reproduction keeps the locked content and settles in sync

## 3. Documentation and archive

- [x] 3.1 Describe the journeys in `tools/tui-qa/README.md`; verify `make spec-check` passes
- [ ] 3.2 Run `openspec archive tui-marketplace-install-respects-locks --yes`
