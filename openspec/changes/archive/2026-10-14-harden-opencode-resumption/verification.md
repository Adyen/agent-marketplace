# Resumption verification

## Scope and outcomes

Integrated main at `e5c71c05fa6c1d4a0d4387c3e02c756702756b1a` without rewriting the OpenCode feature history. Merge commit: `200ad9c5`.

Verified and fixed shared adapter collisions, pre-publication hook budgets in conversion and actual sync, successful handlers declining stdin, unsupported model mappings, and tool override consistency. Short/unclosed frontmatter and null JSON parser panic claims were false positives; valid/error boundary controls remain.

Consumer regressions cover handwritten inline/legacy identities, contained configuration aliases, malformed/oversized/dangling/hard-linked identity inventory, scope-local diagnostics, and required-hook refusal. The same pure budget check now governs conversion and sync, including file/symlink, repo/user, and check-only paths. Refusals preserve installed hooks and permit independent resources.

Required OpenCode hooks remain explicitly unsupported. Native absent, disabled, and initialization-failed plugins are inactive, not enforced. This is not a claim of mandatory native activation.

## Passed gates

- `make validate` with explicit changed-domain root packages and `TEST_FLAGS=-parallel=4`: short-mode tests, vet, zero-issue CI lint, and build.
- `make spec-check`: strict validation, trace, and archive audit.
- `make validate-e2e`: full host E2E and all configuration compatibility scenarios; shellcheck.
- `make tui-qa`: three deterministic driver tests and all 22 real-terminal journeys.
- `make opencode-probe`: all seven required native witnesses.
- Isolated native `TestHarnessCompatibility`: all four OpenCode repo/user and file/symlink scenarios, including native skill, command, agent, and MCP observations.

Native execution used the pinned OpenCode 1.18.34 Linux arm64 executable, offline containers, read-only inputs, private temporary HOME/configuration, and no model credentials. The archive and binary checksums remain documented in `tools/opencode-probe/README.md`.

Historical-client and frontend/container product gates were not rerun. Existing lock/config write formats and frontend behavior are unchanged by the hardening. The scoped root gate is not a fresh full root-suite claim; CI owns that witnessed gate.

## Failures resolved during validation

- Native inventory inspection initially misclassified resource prose as runtime errors and missed startup-created user JSONC. Structured inventory observations and effective-file expectations now pass.
- A full E2E attempt exceeded the tool's 700-second limit. The successful rerun respected the suite's configured 20-minute deadline (833 seconds).
- Three command scenarios hit their fixed 120-second child deadlines under concurrent load. All passed serially; the complete scoped root gate then passed with four test slots.
- The OpenCode backup fixture omitted the destination directory required by the public `Restore` contract. The corrected fixture verifies actual native bytes and excludes unrelated files.
- The TUI journey expected the retired `[x]` marker. The corrected `[•]` assertion passed both focused and full real-terminal runs.
- Container setup initially used an older local Go toolchain or a read-only build-cache path. Cached Go 1.26.9 and private temporary caches resolved setup, without dependency downloads or policy changes.

## Performance evidence

Lifecycle benchmark includes disk I/O for one MCP server, with no network. Five 2-second samples before/after:

| Operation | Before | After | Time result | Allocation result |
| --- | --- | --- | --- | --- |
| sync | 5.350 ms | 5.498 ms | No significant difference | 6013 to 5893 allocs; bytes -1.38% |
| check | 4.512 ms | 4.380 ms | No significant difference | 5310 to 5188 allocs; bytes -2.34% |
| repair preview | 4.988 ms | 4.962 ms | No significant difference | Unchanged |

The identity-read disk microbenchmark compares the previous shared path reader retrospectively against the captured reader on one regular configuration, not lifecycle history: 24.495 to 217.663 microseconds, 728 to 11072 B/op, and 4 to 124 allocs/op. The additional validation is retained. Configuration paths are deduplicated, and aggregate-only work does not perform unused identity inventory.

Compiler hook translation: 1.175 to 1.217 microseconds (+3.53%), bytes +9.92%, and 7 to 9 allocs/op for shared-fragment metadata. Other translation and fragment benchmarks were neutral to faster. Five baseline samples do not establish a 95% confidence interval; these are measured costs, not a broad performance guarantee.

## Adversarial review

Reviewed the intended diff against main, including consumer reachability, failure behavior, scope/ownership, publication generations, and test validity. The delegated review covered 43 reviewable files at `200ad9c5` plus working changes, re-reading the shared budget wiring after it changed.

No confirmed critical/high/medium security finding survived code-level validation. Two low test weaknesses were fixed: isolate budget-diagnostic assertions by code, and pin the embedded adapter's runtime ceiling to the compiler ceiling. The root-query candidate did not reach status/doctor: those consumers use the error-returning destination resolver; the legacy boolean query intentionally reports an unavailable root.

Parent review covered the final test/benchmark/fixture edits after the delegated snapshot. Tests passed as recorded above. Neither this review nor passing gates proves the absence of defects; native behavior evidence is limited to the pinned version and documented cases.
