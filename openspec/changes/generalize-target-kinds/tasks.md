## 1. Tests first

- [ ] 1.1 Add `pkg/harness-compilation/target/accepted_kinds_test.go` `TestCompiledAcceptedKindsMatrix` annotated `// spec: harness-compilation/compiled-targets-declare-the-observed-kind-matrix`, and a completeness case annotated `// spec: harness-compilation/declared-kind-without-an-implementation-fails-validation`; verify they fail on the base commit
- [ ] 1.2 Add `internal/harness/application_target_test.go` `TestAcceptedKindsCompletenessAndLookup` (annotated `application-target-model/current-harness-targets-accept-every-selectable-kind` and `application-target-model/unaccepted-kind-is-refused-before-output`, using a test-only partial target) and `TestCurrentHarnessAcceptedKindOutputUnchanged` (annotated `application-target-model/existing-harness-output-is-unchanged`); verify they fail on the base commit
- [ ] 1.3 Annotate the existing rule compiler tests with `// spec: application-target-model/rule-diagnostics-keep-their-codes` and `TestCompileRefusesPlugin`/`TestCompilePackageProjects` with `// spec: harness-compilation/plugins-compile-only-as-packages`

## 2. Implementation

- [ ] 2.1 Add `AcceptedKinds` to `target.Definition` with the observed matrix and validate it
- [ ] 2.2 Compose `ApplicationTarget` entries in the `internal/harness` registry (`Lookup`, `Targets`, `Accepts`) from compiled definitions and existing adapter factories; keep `TargetInfoMap` derived and the Devin/Codex/OpenCode no-fallback invariant
- [ ] 2.3 Make completeness and catalog checks capability-specific without weakening any current compiled-target check; key `TestGolden_PerResourceKind` positive cases to `AcceptedKinds`
- [ ] 2.4 Run the focused witness command, the existing regression commands listed in the design, `make spec-check`, and `make validate`; verify all pass

## 3. Archive

- [ ] 3.1 Run `openspec archive generalize-target-kinds --yes`
