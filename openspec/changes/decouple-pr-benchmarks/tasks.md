# Tasks

## 1. Wire the independent workflow

- [ ] 1.1 Add the independent Benchmark workflow with the existing pull-request event types, shared `tools/ci-plan` selection, and `run-benchmarks` opt-in; add spec-traced workflow-wiring tests for `benchmark-reporting/eligible-pr-starts-independent-benchmark-workflow` and `benchmark-reporting/documentation-only-pr-skips-unless-opted-in` in `tools/ci-plan`
- [ ] 1.2 Remove the benchmark job from required CI without changing its trigger or required status names; verify workflow separation and independent retry with `benchmark-reporting/failed-required-ci-can-be-retried-while-benchmark-runs` in `tools/ci-plan`

## 2. Preserve benchmark evidence and trust boundaries

- [ ] 2.1 Move the existing exact base/head benchmark steps unchanged, including serial same-runner execution, selection, samples, report, and artifacts; add a spec-traced test for `benchmark-reporting/exact-base-and-head-are-benchmarked-serially` in `tools/ci-plan`
- [ ] 2.2 Preserve the current pull-request permissions and same-repository comment guard; add a spec-traced fork witness for `benchmark-reporting/fork-pr-does-not-attempt-a-privileged-comment`

## 3. Verify workflow integration

- [ ] 3.1 Add a hosted workflow witness proving E2E Gates and Benchmark run independently; run `go test ./tools/ci-plan ./tools/benchmark-report && go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 && make spec-check`
- [ ] 3.2 Add or update spec trace annotations for every new scenario and verify the annotated test files cover the delta
- [ ] 3.3 Archive the change with `openspec archive decouple-pr-benchmarks --yes` in the same pull request as the implementation
