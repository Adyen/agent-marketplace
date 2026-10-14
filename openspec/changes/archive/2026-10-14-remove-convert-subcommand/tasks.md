## 1. CLI removal

- [x] 1.1 Delete `convert.go`, `conversion_publisher.go`, `conversion_secure_unix.go`, and their tests and benchmark files
- [x] 1.2 Remove `newConvertCmd` from the command list and `convert` from the migration prompt skip list
- [x] 1.3 Remove the convert command and flag witnesses from `docs_contract_test.go` and the convert benchmark from `cmd_bench_test.go`
- [x] 1.4 Remove the `TestConvertSource` selection from `scripts/security-race.sh`

## 2. E2E

- [x] 2.1 Port the seven convert-based table scenarios to `e2e/harness_conversion_test.go` calling `hcc.Convert` and `hcc.Materialize`
- [x] 2.2 Remove the convert flag test from `e2e/marketplace_v4_flags_test.go` and the `convert` entry from `schemaIndependentCommands`
- [x] 2.3 Run the factory compatibility rule through `hcc.Convert` and `hcc.Materialize` in `e2e/harness_compatibility_rules_test.go`

## 3. Documentation

- [x] 3.1 Remove `agent-marketplace convert` references from the site, README, AGENTS.md, CODING_STANDARDS.md, skills, and `docs/adoption-guide.md`
- [x] 3.2 Delete `site/docs/how-to/convert-a-harness-layout.md` and its navigation links

## 4. Verification

- [x] 4.1 `go build ./...` and `go -C e2e vet ./...` pass
- [x] 4.2 `TestHarnessConversionSecurity` passes
- [x] 4.3 Command unit tests, compatibility config gate, `make spec-check`, and `make ci-lint` pass
