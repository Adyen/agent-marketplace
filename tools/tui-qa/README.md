# TUI QA Workbench

This directory contains QA-only tooling for black-box terminal validation of the release-equivalent `agent-marketplace` binary. It is not linked into the production binary.

## Invariant baseline

The workbench runs each selected flow in a fresh sandbox and covers:

- bare and explicit TTY launch;
- bare non-TTY help and explicit non-TTY failure;
- `--no-tui` and `AGENT_MARKETPLACE_NO_TUI` suppression on a real TTY;
- stable fixture-backed frames at 80x24 and the supported 50x12 minimum;
- `q`, Ctrl+C, and SIGTERM exits;
- product terminal restoration before the target-exit boundary;
- separate harness-owned PTY recovery and descendant cleanup;
- root-owned organization fallback through `/etc/agent-marketplace/config.yaml` in containment.

A bounded public `sync` prepares a local bare Git registry before each measured flow. Deterministic JSON remains authoritative; the baseline command itself is non-blocking when the result records a product failure.

## Sandbox and containment primitives

Each flow attempt receives a validated fresh root containing isolated HOME, XDG roots, temporary directories, Go/npm caches, workspace, registries, logs, backups, ownership state, generated output, process evidence, and artifacts. The tested-process environment is constructed from an allowlist rather than inherited, and controlled Git/editor shims disable hooks, credentials, signing, SSH prompts, and real editor invocation.

Native runs print a warning because they provide functional isolation for known application-managed paths, not a hostile-code security boundary. `run-container.sh` builds and disposes the pinned Linux workbench with a read-only root and checkout, read-only release binary and build manifest, writable sandbox and artifact mounts only, no runtime network, dropped capabilities, no-new-privileges, a PID limit, and symlink-escape denial. The containment self-test runs before product flows.

## Results and artifact contracts

`results.v1.json` is the deterministic authority for flow status and exit code. It records immutable invalid reasons, build identity, classifier selections, structured expected/observed failure classes, product restoration separately from harness recovery, assertions, bounded side effects, transformed evidence hashes, and artifact policy. Semantic validation is applied after JSON Schema validation.

Non-raw evidence is always terminal-sanitized, credential-redacted, rescanned, bounded, and hashed during collection. Raw ANSI remains local by default; exceptional upload requires both a synthetic/no-secret attestation and a successful secret scan and remains explicitly marked unsanitized. PR CI uploads only results, report, projected build identity, containment output, and transformed flow evidence with seven-day retention; the `raw/` tree and release binary are excluded.

`exploratory-supplement.v1.json` is the additive handoff for the bounded exploratory agent. It is bound to a deterministic results hash, sanitizes advisory text, may request additional optional flows, and cannot contain deterministic status fields or override required outcomes.

## Controlled detector fixtures

Fixture-only sessions use the selected `node-pty` transport with the same versioned wrapper boundary and direct `@xterm/headless` product-cell rendering. Ten deliberately broken fixtures prove blank frame, real right-edge clipping, hang timeout, control injection, unexpected exit, leaked descendant, panic, product restoration, returned-error, and SIGKILL classifications. Product bytes and cells are evaluated before harness recovery; owned-PTY reset bytes, lifecycle probes, and complete process cleanup are evaluated separately.

Each completed run writes immutable `results.json`, a generated `report.md`, transformed per-flow evidence, bounded local-only raw traces, and a projected build manifest beneath `.local/agent-marketplace/qa/tui/<run-id>/`.

## Driver decision

The selected driver is `@microsoft/tui-test@0.0.4` (MIT, published 2026-04-04). Its `node-pty` boundary and `@xterm/headless` cell model provide semantic locators, terminal cells, resize, keyboard, mouse, signal, and raw trace capabilities in one adapter. `node-pty@1.2.0-beta.11` is a direct exact dependency rather than an optional transitive dependency, so installation cannot succeed while silently omitting the PTY backend. `@xterm/headless@6.0.0` is also direct and exact for fixture-only product-cell reconstruction. All selected versions exceed the repository's seven-day minimum release age.

A byte-oriented Go PTY/expect implementation was rejected for this layer because it would require building and maintaining a second terminal emulator or reducing assertions to raw byte matching. Raw bytes remain evidence, but the workbench also needs terminal-cell semantics.

The selected driver currently resolves deprecated `glob@10.5.0` within its declared dependency range. `npm audit` reports no vulnerability; this layer does not force an unvalidated major-version override outside the driver's range.

Node is pinned by `.node-version`; package metadata accepts that pinned Node 24 release line. The lockfile contains exact resolved versions and integrity hashes. `allowScripts` permits only the reviewed exact `@swc/core` binding validator and `node-pty` prebuild validator/post-install cleanup.

## Commands

```sh
npm ci
npm run unit
npm run driver
npm run check
AM_TUI_QA_RUN_CLASS=baseline npm run qa
```

From the repository root:

```sh
make tui-qa-check
make tui-qa
make tui-qa-contained
```

`make tui-qa-contained` is the canonical baseline because it exercises the fixed organization path and Linux security boundary. `make tui-qa` is useful for trusted native debugging and always states its weaker isolation claim. Set a unique `AM_TUI_QA_RUN_ID` to make artifact locations predictable; completed run IDs are immutable and cannot be reused.

`npm run check` executes unit tests, production TypeScript compilation, test typechecking, and the real PTY driver smoke. A green typecheck without the PTY smoke is not considered driver validation.

## Restoration evidence

`pty-wrapper` writes `AM_TUI_QA_TARGET_EXIT_BOUNDARY` before any harness reset. Product restoration must be evaluated only from bytes before that boundary and the captured `targetAtExit` termios state. Harness cleanup after the boundary is reported separately as `harnessRecovery`; it cannot satisfy a product restoration assertion.
