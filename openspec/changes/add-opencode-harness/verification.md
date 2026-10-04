# OpenCode implementation verification

The change adds opt-in native skills, commands, subagents, MCP servers and
command PreToolUse hooks through the existing harness engines. Default harness
selections and existing configuration/lock write formats are unchanged.

## Completed checks

- `make validate`: selected short-mode root tests, non-mutating lint and build.
- `make validate-e2e`: complete host E2E and all-target configuration compatibility;
  ShellCheck passed. OpenCode lifecycle/configuration checks were repeated after
  the final scope-transition fix.
- `make spec-check`: strict pinned validation and scenario/archive audits.
- `make tui-qa`: all 14 real-terminal journeys, including OpenCode selection and
  installation. Typechecking also passed.
- `make opencode-probe`: all seven witnessed native tests passed with the pinned
  OpenCode 1.18.34 Linux arm64 executable. The gate proves native resource
  discovery in both modes, active/inactive callback controls, installed hooks,
  TodoWrite matching, handler/output limits and complete ordered invocation at
  the 63/64 boundaries, plus refusal at 65.
- Focused compiler, ingestion, matrix/golden, destination, materialization,
  backup, repair, resource assessment and CLI checks passed.

Native evidence covers the cases stated in the compatibility note. MCP/nested
subagent hook coverage, global required enforcement and forced native-parent
cleanup are outside the first-release contract. Repository and machine
selections sharing a native override refuse before writes. A file-only override
keeps cleanup of distinct resource roots while reconciling shared MCP once.

## Review

OCR reviewed 70 changed code files, including tests. Test filenames and the
compiler target directory were mapped in isolated review copies so default
exclusions would not omit their logic. The initial 16 confirmed findings were
fixed. Follow-up review prompted scope-transition cleanup and a stronger native
agent-mode witness; the missing repair-binding context was checked separately.
A final three-file review of machine-only empty-sync cleanup returned no
findings; its regression failed before the fix and passed afterward.

OCR excluded Markdown documentation/specification files, the Makefile, and
YAML/JSON/Markdown/JavaScript fixture trees under
`pkg/harness-compilation/testdata/`. Those files were inspected locally; matrix,
golden, strict spec and native checks exercise their relevant content. Raw
review and execution logs are retained privately and are not committed.

The separate pre-push adversarial review checked consumer reachability from
registry selection through conversion, install/sync/check/status/doctor/repair
and TUI; unsupported or malformed resource behavior; writes during inspection;
handwritten aggregate/adapter ownership and scope boundaries; and whether
regression tests exercise actual outcomes. No unresolved actionable defect was
identified in that review. This is scoped evidence, not proof of absence of bugs.

Reviewed code content SHA-256 (sorted paths plus NUL-delimited file contents):
`7336fe1a26ef31fc835295b1ce0f72ac905b92047a0b5a2469e35390b6066fc0`.

## Measurements

Representative existing Claude ingestion and Codex MCP controls were measured
against the proposal revision, with three 300 ms samples per case. Allocation
counts stayed at 263 and 578 respectively; the new optional extension adds
approximately 32 bytes to the ingestion fixture. Timings varied under concurrent
validation and do not establish a speedup or regression.

New OpenCode benchmarks cover every translated kind, a 64-command/agent source,
and repeated sync, check and repair preview. Their raw samples remain private.
They establish an initial measurement set rather than a performance guarantee.
