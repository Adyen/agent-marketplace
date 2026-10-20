# Coding Standards

Read this document for every code change. It defines the review criteria and implementation requirements for platform support, design, tests, observability, logging, and performance. Tool-enforced formatting and lint rules are intentionally not repeated here; project architecture and session workflow remain in [AGENTS.md](./AGENTS.md).

## Platform scope

- Support Unix-like systems only: macOS and Linux. Windows build or runtime compatibility is not a requirement.
- Do not add Windows-specific branches, build tags, path handling, abstractions, or tests unless the project's supported-platform decision changes.
- Remain portable across macOS and Linux. Use `filepath` for host filesystem paths and `path` for slash-separated artifact or `fs.FS` paths.

## Design

- Keep each policy in one authoritative place. Centralize target definitions, mappings, defaults, path rules, and diagnostic semantics instead of repeating switches or constants across packages.
- Extract genuinely shared behavior into focused helpers. Do not duplicate logic, and do not add extension points or abstractions for hypothetical future uses.
- Use names that expose domain intent. Prefer domain types over groups of loosely related primitives when the grouping carries invariants.
- Keep modules cohesive and boundaries explicit. Pure resolution and compilation logic must stay separate from filesystem, process, and network mutation.
- Preserve deterministic and idempotent behavior. Identical inputs must produce identical ordered output and bytes; sort map-derived output explicitly, and avoid rewriting files whose desired bytes are unchanged.
- Treat paths, roots, and symlinks as untrusted input. Validate containment and reject traversal or symlink escapes before filesystem mutation.
- Fail closed at destructive boundaries. Malformed, ambiguous, or unowned input must not trigger overwrite or prune behavior. Contain a failure to the narrowest safe unit so valid independent work can still proceed when the contract permits it.
- Never hide unsupported or lossy behavior. Return a stable coded diagnostic and omit the unsafe or unrepresentable output rather than silently dropping data or fabricating support.

## Security boundary review

For changes to ingestion, compilation, config loading, or filesystem publication, reviewers must check the complete input-to-output path, including custom decoders, fragment merging, CLI sync, and direct conversion. A safe helper is insufficient if another entry point bypasses it.

- **One interpretation of typed fields.** Reject exact duplicate JSON keys and case-colliding aliases of modeled fields before decoding can discard the ambiguity, including nested structs and custom unmarshallers. A supported singleton alias must be consumed as modeled data, not also retained as an extension. Keep arbitrary dictionary keys case-sensitive; do not conflate environment-variable names, resource names, or unrelated extension keys. Canonical artifact producers must not emit ambiguous payloads or silently discard conflicting metadata to make validation pass.
- **Typed execution authority.** Reserve hook command, type, matcher, prompt, timeout, event, and nested-handler fields even when their typed values are empty or absent. Extensions and native group extras must not supply these fields through alternate spellings. Parse valid native handler groups into typed resources rather than depending on executable data surviving in extras. Preserve unrelated extensions and report rejected conflicting fields without including their values.
- **Descriptor-based filesystem safety.** Anchor writes to the authorized scope and validate the opened file, not only its lexical path or an earlier `Stat`/`Lstat`. A later path-based open can invalidate a symlink check; atomic rename alone does not establish containment. For automatic in-place updates, use no-follow opens and one descriptor, reject non-regular and multiply linked files, and prevent FIFO opens from blocking. Preserve explicitly supported config symlinks resolving to regular files rather than imposing an indiscriminate symlink ban.
- **Private creation, faithful updates.** New sensitive aggregates must have private permissions throughout compilation, fragment merging, staging, and publication. Explicit fragment modes must survive merging; incompatible modes must fail closed. Preserve existing shared-file permissions and handwritten data, and do not let a current umask silently change an existing file's mode. Identical-output sync must not rewrite or chmod the file. Keep mandatory backups private.
- **Bounds at every read boundary.** Root configs, recursive includes, alternate loaders, and overlays must use the same applicable size policy. Require that opened files are regular files where that is the input contract, reject oversized input before parsing, and retain a bounded reader even after checking size to handle growth. Depth or cycle checks do not substitute for byte or total-work bounds; any new aggregate budget needs an explicit policy decision rather than an arbitrary threshold.
- **Evidence that exercises the boundary.** Require a failing regression test and a valid control through the actual consumer. Cover both alias orders, empty typed values, nested fields, legitimate native groups, external/dangling symlinks, hard links, special files, size boundaries, permissions, and byte/mtime no-ops as applicable. Assert the intended error identity and unchanged external bytes, not just any failure. Isolate umask changes and potentially blocking probes in bounded child processes. A preservation test must prove that the intended content update occurred; an include test must not duplicate its selection in the containing config. Use each target's real loader and native installation path, not differently labeled calls to one helper or plugin output as a substitute for native sync.

Consumer-config boundary review must verify exact-v4 roots in all four layers (including organization and personal-local), accepted versionless/exact-v4 included fragments, and file/field-specific `v4.api-version-required` or `v4.api-version` refusals across readers, direct editors, staged reloads, init, ad-hoc acquisition, and CLI/TUI entry points. Validate all required input before new acquisition or config/lock/harness mutation. Preserve established pending-journal handling and ordinary logging rather than imposing a blanket no-write claim. Do not restore a legacy decoder/editor, migration command, or automatic conversion prompt. Keep the internal v4 runtime adapter and independent lock/hash/operator/API/artifact/native schemas.

Apply the existing benchmark requirements to added validation on hot paths. Report its cost explicitly; do not remove safety checks to obtain a faster result.

## Tests

- Test observable contracts, not the implementation's restatement of itself. Expected values must be independently authored; do not derive them with the production helper, table, mapping, or algorithm under test.
- Do not write tautological or vacuous tests. Every test must be capable of failing for the regression it claims to cover. Negative, no-op, filtering, and redaction tests need a positive or non-vacuity control proving that the exercised path had something to reject, preserve, redact, or change.
- Start bug fixes with a regression test that fails against the buggy behavior, then make it pass with the fix.
- Assert the complete externally relevant outcome, including negative side effects. Filesystem failure tests must verify that user bytes remain unchanged and that no temporary or staging residue survives.
- Assert machine-readable contracts with stable diagnostic codes, typed errors, or `errors.Is`/`errors.As`; do not couple tests to incidental error prose.
- Use table-driven subtests for behavior matrices and byte-exact golden tests for generated layouts. Share fixtures and assertion helpers rather than repeating setup.
- Isolate tests from the developer's machine. Use temporary directories and scoped environment variables; default unit tests must not read or mutate the real home directory or depend on optional third-party executables or external network services.
- Follow the E2E test selection rules below: prefer declarative cases for standard setup/run/assert workflows and standalone tests only when the framework cannot express the setup.

### Property-based tests

- Use [`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) for structured invariants on internally consistent values (configs, documents, IR, ownership state). Use native Go fuzzing for raw-byte parser boundaries instead.
- Property oracles must be independently authored, like expected values in example tests: derive them from the contract, never by re-running the production algorithm under test.
- Constrain generated inputs per case so each subtest covers the interesting region by construction; do not generate broadly and then filter most inputs away.
- Assert with `errors.Is`/`errors.As` and stable diagnostic codes, as elsewhere; do not match error prose.
- Rapid is test-only. The `go list -deps` graph guard in `cmd/agent-marketplace` enforces that Rapid never links into the shipped binary; production code must not import it.
- Never commit Rapid failfiles (`testdata/rapid/` is ignored). When a run shrinks a counterexample, convert it into a plain example regression test in the same change as its fix.

### E2E test suite

The `e2e/` directory contains E2E tests that validate end-to-end CLI behavior by running the built binary in isolated temp-directory sandboxes. Registry fixtures are local bare Git repositories reached through `file://` URLs, so the suite runs directly on Ubuntu in CI; local runs use Docker by default for additional host isolation.

There are **two types** of E2E tests:

1. **Declarative (table-driven)** — defined as `TestCase` structs in `e2e/table_test.go`. These are the preferred format for tests that follow a standard setup/run/assert pattern: write config, run a command, check output and filesystem. Use these when the test fits the `Steps` model (actions + assertions on a single workspace).
2. **Standalone** — individual `func TestXxx(t *testing.T)` functions in separate files (e.g. `e2e/install_test.go`). Use these when the test requires non-standard setup that the declarative model doesn't support, such as running commands from a non-default working directory or skipping git repo initialization.

#### Running

```bash
make e2e                  # runs inside Docker (isolated from host env)
E2E_SKIP_DOCKER=1 make e2e  # for CI runners that cannot spawn Docker
E2E_PARALLEL=8 make e2e  # control concurrency (default 4, set to 1 for serial)
```

#### Architecture

- **Process-based sandbox**: each test gets its own temp root with `home/`, `workspace/`, `repos/`, `etc/` directories. Subprocesses strip inherited `AGENT_MARKETPLACE_*` settings and override `HOME`, the XDG state/config/cache roots, `CODEX_HOME`, the organization-config test path, and a sanitized `PATH`, so application-controlled paths and policy cannot escape the sandbox or vary with the host. Fixture-specific environment overrides are applied explicitly after this baseline.
- **Path mapping**: test table uses container-style paths (`/root/...`, `/workspace/...`) which are automatically resolved to the temp sandbox by `Env.resolvePath()`.
- **Declarative test cases**: defined as `TestCase` structs in `e2e/table_test.go` with `Registries`, config layers (`GlobalCfg`/`RepoCfg`/`LocalCfg`/`OrgCfg`), and `Steps` (actions + assertions). `{{REG:name}}` placeholders are resolved to `file://` URLs of seeded bare git repos.
- **FIFO concurrency handshakes**: use `openFIFOWriter` so writer setup is bounded, reports a subprocess that exits before opening its reader, and keeps the writer descriptor close-on-exec; use `waitForProcess` to bound subprocess completion.

#### When to add a test

Add a new `TestCase` to `testTable` in `e2e/table_test.go` when:
- Adding a new CLI command or flag that should be validated end-to-end
- Fixing a bug that involves config merging, materialization, precedence, or collision handling
- Changing resource selection/exclusion semantics (`required`, `default`, `tags`, `exclude`)

Add a standalone test function in a separate file when:
- The test needs non-standard setup (e.g. no git repo, custom working directory)
- The test uses `RunAMFrom` or other framework features not exposed through the `Steps` model

Use the structured fixture builders in `e2e/harness_test.go` (`Skill`, `Command`, `Subagent`, `MCPServer`) to define registry contents. Keep `Desc` fields as Given/When/Then descriptions.

#### Command coverage guard

Every public command and flag that interprets consumer configuration must have an actual successful behavioral E2E invocation in a v4 workflow. Every present consumer root in that invocation, including organization and personal-local, must declare exact `agent-marketplace/v4`; first-run workflows may create absent roots as v4. Commands that never read layered consumer configuration (`completion`, `help`, Marketplace `serve`) retain successful configuration-independent witnesses. The `e2e`-tagged binary records each invocation's command, flags, starting `apiVersion` for each layer, and outcome. Help-only runs, refusals, relabeled legacy records, or success with an unsupported root cannot satisfy consumer operational coverage. After a complete, passing `make e2e` run, the suite fails if any requirement lacks its successful invocation. `e2e/testdata/command-coverage-gaps.txt` remains empty; do not add exceptions for legacy-support removal.

The normal Marketplace E2E launch gate must execute required refusal witnesses for unsupported/versionless roots in every layer and explicit non-v4 included fragments. Seed valid selections, existing locks and owned output, and acquisition counters so no-write/no-acquisition claims are non-vacuous. Pending-journal tests must distinguish safe replay of already-admitted writes from refusal of new work; crash-recovery success tests exercise supported v4 mutations, not consumer migration.

The pre-v4 success `upgrade-gate` and CI job are retired. Preserve their independent lock-byte/source-revision, scope, collision, and ownership assertions using v4 fixtures. Keep `released-client-gate` pinned to checksum-verified stable v4.1.4 with a shared v4 fixture and isolated actors. `E2E Gates` passes only after successful planning and both Marketplace/released-client jobs succeeding when selected, or both intentionally skipping when unselected; failures, cancellation, missing witnesses, and unexpected skips fail acceptance.

### TUI test ownership

Go tests under `internal/tui` own model state, asynchronous behavior, rendering contracts, and terminal-guard details. Go E2E tests own configuration and filesystem policy matrices. `tools/tui-qa` owns focused, direct real-terminal user journeys and must not duplicate those matrices or introduce a separate inventory, classifier, result authority, or retry system.

Go tests never replace a journey. Every change to TUI behavior adds or extends a `tools/tui-qa/tests/` journey that drives the changed flow through the real binary and asserts what the user sees (and the files written, when the flow writes them). Extend an existing journey when it already reaches the flow; otherwise add one file per interaction. `specaudit gate` fails pull requests that change non-test `internal/tui/` or `cmd/agent-marketplace/tui*.go` files without a journey change, unless a commit carries `TUI-QA-Impact: none — <reason>` for a change with no user-observable TUI effect.

TUI QA commands must run without direnv or organization-specific infrastructure. Optional local values may come from the environment or ignored `.envrc.local`; tracked commands, reports, and fixtures must not contain organization-specific image, certificate, registry, or infrastructure identifiers.

## Errors and observability

- Wrap errors with the failed operation and relevant context while preserving the cause with `%w`. Branch on error identity, not message text.
- When a span owns downstream work, pass the context returned by `StartSpan` to that work so its spans inherit the parent. Discard the returned context only for leaf spans.
- Treat stable diagnostic codes and severities as public behavior. Add or update tests whenever either changes.

### Logging

The CLI configures a process-wide `slog` logger before any subcommand runs (see `cmd/agent-marketplace/main.go`). Instrument new features and changes with structured logs via the standard `slog` default logger (`slog.Info`, `slog.Debug`, `slog.Error`, ...). At minimum, log meaningful operation boundaries, decisions, and error paths with relevant fields such as counts and durations. Never log credentials, tokens, headers, or sensitive payloads.

- **Defaults live in code.** The `Default*` constants in `internal/config/config.go` are the single source of truth for the logging runtime (`internal/logging`). Keep them there.
- **Do not seed a `logging:` block into the generated config skeleton.** The skeleton carries only required global fields (`apiVersion`, `harnesses`); logging is opt-in override only. Users override via `~/.agents/config.yaml` (`logging:`) or `AGENT_MARKETPLACE_*` env vars. Precedence is **env var > config file > code default**.
- Current strategy and overridable keys: [logging reference](./site/docs/reference/logging.md). Historical `docs/` leads are not current consumer-format guidance.

## Performance and benchmarking

This project treats performance as **data-driven, not narrative-driven**. Optimizations and refactors that touch hot paths must be accompanied by benchmarks; "it feels faster" is not acceptable evidence.

During every implementation cycle:

1. **Spot a hot path → write a benchmark.** Any code that runs on a user-visible latency path (each keystroke, every `sync`, every `status`, every `Resolve`, every `DiscoverSkills` invocation) earns a `func Benchmark…(b *testing.B)` in the same package the first time it is modified. No silent hot paths.
2. **Measure before you change.** Capture a baseline with `go test -run '^$' -bench '^BenchmarkFoo$' -benchtime 2s -count 5 -benchmem | tee before.txt` *before* the optimization lands. Without a baseline the change is unverifiable.
3. **Measure after.** Re-run the same command into `after.txt` and diff with `benchstat before.txt after.txt`. Post the delta (ns/op, B/op, allocs/op) in the PR description or commit message; no delta ⇒ not merged.
4. **Regression guard.** The benchmark stays in the tree. Anyone who later regresses the hot path will see it in CI or when running `make bench` locally.
5. **Micro-benchmarks live next to the code.** Name the file `*_bench_test.go` so it is discoverable and can be excluded from `go test` wall-clock budgets when needed.

Keep setup out of benchmark timing, exercise realistic workloads, and report allocations. Do not trade correctness, determinism, or safety for an unmeasured optimization.

End-to-end lifecycle benchmarks must state whether disk I/O and network requests are included. Keep those operations in the timed path using isolated filesystems and local endpoints; if a mock or no-op deliberately narrows the measurement, name and document it as a narrower benchmark rather than presenting it as end-to-end.

Review the `efficient-go` skill (`~/.factory/skills/efficient-go/SKILL.md`) for the Three Rs method (Reduce / Reuse / Recycle allocations), pre-allocation patterns, and `sync.Pool` guidance when proposing a performance change.
