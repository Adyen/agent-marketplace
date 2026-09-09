# Process-isolated command tests

Status: proposed; this document does not implement the runner.

Audience: maintainers of the command test suite.

## Problem and evidence

The command package mixes unit tests with Git-backed integration scenarios. Its tests mutate process-global state: environment variables, working directory, configuration caches, package-level fault hooks, logging, and telemetry lifecycle state. Separate temporary directories do not isolate those globals. Adding `t.Parallel()` to the existing bodies would introduce cross-test interference.

On the lockfile branch at `50693f4`, the committed command suite contains 408 top-level tests versus 324 on local `main`. The subsequent organization-lock regression adds one more. Static searches counted 226 references to the common Git fixture helper and 255 references to the commit/push helper, including their definitions. There are no `t.Parallel()` calls in the command test files. These are source counts, not measured runtime invocation counts; table-driven subtests can multiply the work.

An uncached command-package run took 483.951 seconds on the development machine. Simplifying fixture setup from working initialization, empty seed commit, and bare clone to working initialization and bare initialization produced a 452.951-second run. That roughly 6.4% difference is an observed single-run comparison, not a statistically established speedup. The representative five-iteration regression run also improved, from 7.756 to 7.333 seconds.

A later pre-commit run of the command package completed in 228.937 seconds without another fixture change. This large variation means the earlier 31-second difference cannot be confidently attributed to the fixture simplification alone. Controlled repeated measurements are required before claiming a causal speedup.

Test-level timing showed distributed costs rather than one timeout: organization update and manifest-tampering matrices took approximately 11 seconds each; removal matrices took 9–10 seconds. This supports parallelizing independent integration scenarios, but does not prove how much time Git, filesystem operations, or individual production functions consume. No comparable `main` runtime was measured.

## Decision

Use the operating-system process as the isolation boundary for command scenarios that need process-global state. Retain three distinct test categories:

| Category | Execution model | Appropriate coverage |
|---|---|---|
| Pure unit test | In-process, parallel when inputs and dependencies are local | Argument validation, formatting, selection helpers, pure lock planning |
| Command scenario with internal hooks | One selected scenario per child test process | Fault injection, typed errors, internal transaction assertions, cache hooks |
| Black-box CLI scenario | Existing process-based E2E harness | Exit status, output, config/lock bytes, native files and permissions |

The existing E2E harness remains the owner of ordinary declarative CLI journeys. A child-test runner is not a second E2E framework: it provides process isolation for tests that need access to unexported Go functions and hooks. It must not grow another config fixture DSL, report format, or resource inventory.

No production dependency-injection rewrite is required for this proposal. Child processes keep the real configuration loader, Git acquisition, transaction machinery, and materializer under test.

## Execution model

```text
parent command test binary
  TestScenarioA wrapper -- parallel slot --> child test binary: ScenarioA only
  TestScenarioB wrapper -- parallel slot --> child test binary: ScenarioB only
  pure unit test        -----------------> in-process assertions

child ScenarioA
  private HOME / workspace / XDG roots / organization authority / Git origins
  existing scenario body
    set environment and cwd
    install internal hooks
    execute one or more Cobra commands sequentially
    assert outputs, typed errors, and filesystem effects
  cleanup and exit
```

The parent launches its own executable, obtained through `os.Executable()`. It does not build a test executable per scenario or invoke `go test` recursively. The child executes the same instrumented test binary and therefore retains access to package-private helpers and fault hooks.

A scenario wrapper has two paths. In the parent it enters `t.Parallel()`, launches the child, and translates the result into the parent test result. In the selected child it runs the scenario body synchronously, without calling `t.Parallel()`. All setup that changes globals belongs inside the child path, including fixture construction and `t.Setenv` calls. This avoids Go's prohibition on combining parallel tests with process-global environment changes.

The child selector is an exact, anchored test-name pattern. Every slash-separated component is regexp-escaped and anchored independently so a name such as `TestE2E` cannot accidentally select `TestE2ECodex...`. A private marker carries the expected full scenario name. The marker is removed from inherited input before the parent assigns it; it is test plumbing, not a production CLI flag. The child must refuse a selector mismatch and must not recursively spawn another child.

A top-level matrix initially stays together in one child. Its subcases remain serial, preserving current fixtures and cleanup behavior. Independently initialized matrix cases can later become separate child scenarios when measurements justify the extra startup cost. Cases that intentionally share state stay together.

## Parent and child lifecycle

### Parent responsibilities

The parent prepares immutable launch inputs and owns process lifetime, output capture, and scheduling. Parent wrappers must not change cwd, environment, the default logger, or package hooks.

Existing `TestMain` initialization needs an explicit audit. It currently installs isolated home and organization settings and disables command logging. Legacy serial tests may continue to use that initialization during migration. Child launch settings must nevertheless be captured from a stable baseline, not read from environment or cwd while other parent tests execute. A child must not inherit a sibling test's temporary home or logger configuration.

Normal Go scheduling allows legacy top-level serial tests to finish before parallel wrappers resume. That provides an incremental migration path, but it is not permission for future parallel parent tests to mutate globals. Parent setup remains byte-neutral after test execution begins.

### Child responsibilities

The child initializes its own `TestMain` state before executing the selected body. Its home, organization override, cache roots, and durable state are independent of the parent's test home. Existing per-test overrides remain valid inside that child.

The child runs the complete scenario lifecycle: arrange, command execution, assertions, hook restoration, and cleanup. A multi-command scenario intentionally shares state between its commands; only unrelated scenarios are isolated. Production file locks remain real. A contention scenario creates its participants against the same private root so the test still exercises actual contention.

A structured completion signal distinguishes success, skip, and failure. A zero process exit alone is insufficient: an incorrect selector can otherwise run no tests and falsely pass. The child records the exact scenario name and terminal result to a private control artifact separate from captured command output. Missing or malformed completion is a parent failure. Panics and nonzero exits fail even if a partial completion artifact exists. Nested assertion output is attached to the parent failure under its existing test name.

### Cancellation and cleanup

Each child receives a deadline bounded by the parent test deadline, with time reserved for cleanup. On macOS and Linux the runner uses a separate process group and terminates the group on timeout or cancellation, then waits for it to exit. `CommandContext` alone does not guarantee that spawned Git or shell grandchildren are reaped. Output pipe waits must also be bounded so inherited descriptors cannot hang the parent.

The parent owns a unique sandbox root and removes it only after the child and its descendants have stopped. Cleanup covers journal files, read-only snapshots, and interrupted scenarios. On failure, bounded stdout/stderr tails and the scenario name are reported; optional retained artifacts must be explicitly requested and must not contain host secrets.

## Isolation contract

| State | Required boundary |
|---|---|
| Working directory | Child `Cmd.Dir`; any later `os.Chdir` remains child-local |
| HOME and XDG config/cache/state | Unique scenario-owned paths |
| Organization config | Existing test-only override, never real `/etc` |
| Codex paths | Explicit sandbox `CODEX_HOME`; skills still use sandbox HOME |
| Config and repo-root caches | Fresh child address space, then normal scenario resets |
| Logging and telemetry | Fresh child globals; controlled settings and local test endpoints |
| Git origins, objects, refs and worktrees | Per-scenario mutable storage |
| Git environment | Sanitized inherited repository variables and user/system configuration; fixture-local identity |
| PATH and helper binaries | Existing sandbox policy, explicit executable mocks |
| Fault hooks and function variables | Child address space only |
| Ownership and dependency locks | Real locks under scenario roots; deliberate sharing only within a scenario |
| Network listeners | Loopback, dynamically assigned ports; no external services |

Environment construction is a single authoritative policy, not a growing list of ad hoc overrides in test bodies. Reuse `internal/testutil` where suitable and the existing E2E isolation policy as the reference. The E2E directory is a separate Go module and its helpers are currently test-only, so sharing implementation requires a deliberate package/module boundary change; do not assume those helpers can be imported directly into the command package. Any extracted common policy must replace its old implementation rather than duplicate it.

This is isolation from accidental process-global interference, not a security sandbox against malicious test code. Production containment and trust tests remain necessary.

## Scheduling and developer experience

Use Go's parallel-test scheduler as the concurrency authority for child wrappers. Do not add a second semaphore to this runner. The suite entry point should initially select a conservative parallelism such as four, while allowing a serial diagnostic run. The final default is chosen from measurements on macOS and Linux CI rather than CPU count alone: each child can itself launch concurrent Git work.

Existing test names remain stable for `-run`, and an ordinary filtered test invocation selects the same wrapper in the parent. Repeat count and shuffling apply at the parent scheduling layer. A child must not inherit the parent's repeat count and multiply the scenario executions. Verbose child output is captured and associated with its parent scenario rather than interleaved across processes.

Race instrumentation is preserved because the child uses the same executable. Coverage is more subtle: child counters are not automatically included in the parent's coverage report. Before enabling isolated cases in a coverage job, the implementation must collect supported Go coverage artifacts from children and merge them, or retain an explicit serial in-process coverage mode. The initial proposal chooses serial coverage mode to avoid introducing a coverage-merging subsystem. Coverage mode must exercise the same bodies and must never silently omit child scenarios.

## Migration boundaries

The first migration should cover a small representative set: one read-only inspection, one multi-command lock lifecycle, and one fault-injection scenario. This proves isolation and error propagation before converting expensive matrices.

Manifest-tampering and organization update matrices are useful subsequent candidates because their measured runtime is high and their assertions already have clear scenario boundaries. Ordinary setup/run/assert cases that need no internal hook instead belong in the declarative E2E table. Existing E2E concurrency policy stays separate from the command package's scheduler.

Migration preserves each test's observable assertions, including no-write guarantees, exact bytes, trust failures, and absence of staging residue. Equivalent coverage must pass in its new location before the original case is removed. Test naming and review should make the move explicit; duplicating all scenarios indefinitely would increase total work even if wall time fell.

Pure tests can adopt `t.Parallel()` only after confirming that all callees avoid globals. Fixture reuse is limited to immutable assets such as the test executable and read-only PATH scaffolding. Sharing writable Git repositories, ownership databases, or snapshot caches across unrelated scenarios is out of scope.

## Alternatives and trade-offs

- **Add `t.Parallel()` everywhere:** invalid with the current cwd, environment, and hook mutations; temporary directories alone do not help.
- **Guard tests with a global mutex:** avoids interference but preserves serialization.
- **Inject every runtime dependency:** could make in-process concurrency possible, but is a much larger production refactor. Focused seams remain worthwhile when independently justified.
- **Move everything to E2E:** loses typed-error and fault-injection coverage or forces unsafe test controls into the production CLI. Move only genuinely black-box cases.
- **Shard by manually maintained name lists:** can improve CI runtime but risks omissions and duplicated execution, and does not improve normal local invocation. It is not the default design.
- **Reuse a pool of child processes:** reduces startup overhead but reintroduces global-state reset requirements. One scenario per process is easier to reason about.

Process startup and isolated filesystem work still cost time. The goal is lower wall-clock latency without weakening fidelity, not less total CPU usage or a promised linear speedup.

## Acceptance evidence

The runner is ready for wider adoption only when these properties are demonstrated:

- Two child scenarios overlap while using different cwd, environment, hooks, config caches, and XDG roots; each observes only its own sentinel values.
- A failing assertion, panic, missing selector, skip, timeout, and orphaned subprocess each produce the correct parent result and bounded cleanup.
- A deliberately held lock blocks or fails only the intended participant, proving that process isolation has not mocked away contention.
- Parent environment, cwd, and host sentinel files remain unchanged, including when children fail.
- Exact filtered runs, repeated runs, and shuffled runs select every intended scenario once per repetition.
- Race-enabled runs pass, and serial coverage mode includes the migrated scenario bodies.
- Original functional assertions remain intact, with no new external-network dependence or access to personal config.

Performance evidence must compare the same set of tests, uncached, on the same machine, with repeated runs. Record median and spread for serial baseline and parallelism 1, 2, and 4; also record peak memory, test counts, failures, and subprocess cleanup. Fixture creation, Git processes, and disk I/O remain inside the timed workload. Aggregate subtest and parent elapsed times must not be double-counted.

The previously observed 452.951-second result is a reference point, not an acceptance threshold. A successful pilot must show a repeatable wall-time reduction on the representative set and no disproportionate memory or flakiness regression. Full-suite measurements determine whether the migration should expand.

## Related code and policy

- [Command TestMain and execution helper](../../cmd/agent-marketplace/main_test.go)
- [Current global cwd and Git fixture helpers](../../cmd/agent-marketplace/list_test.go)
- [Existing E2E sandbox](../../e2e/framework_test.go)
- [E2E executable lifecycle](../../e2e/suite_test.go)
- [Declarative E2E scenarios and scheduler](../../e2e/table_test.go)
- [Coding standards](../../CODING_STANDARDS.md)
