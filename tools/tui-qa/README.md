# TUI QA Workbench Core

This directory contains QA-only tooling for black-box terminal validation of `agent-marketplace`. It is not linked into the production binary.

## First-layer scope

This layer provides:

- exact Node and npm dependency resolution;
- a real PTY and headless terminal through `@microsoft/tui-test`;
- bounded process output;
- timeout and SIGINT/SIGTERM/SIGHUP cancellation;
- TERM-to-KILL cleanup of the owned process group and descendants observed across process-group boundaries;
- an explicit target-exit boundary separating product output from harness recovery;
- terminal-control sanitization for captured cell data;
- an executable driver smoke covering initial size, resize, special-key input, bracketed paste bytes, mouse input, signal delivery, cell capture, and harness-owned PTY recovery.

## Sandbox and containment primitives

Each flow attempt receives a validated fresh root containing isolated HOME, XDG roots, temporary directories, Go/npm caches, workspace, registries, logs, backups, ownership state, generated output, process evidence, and artifacts. The tested-process environment is constructed from an allowlist rather than inherited, and controlled Git/editor shims disable hooks, credentials, signing, SSH prompts, and real editor invocation.

Native runs provide functional isolation for known application-managed paths; they are not a hostile-code security boundary. `run-containment-selftest.sh` independently proves the Linux boundary supplied by a caller-provided image: read-only root and checkout, read-only binary, writable artifact mount, no runtime network, dropped capabilities, no-new-privileges, PID limit, and symlink escape denial.

## Results and artifact contracts

`results.v1.json` is the deterministic authority for flow status and exit code. It records immutable invalid reasons, build identity, classifier selections, structured expected/observed failure classes, product restoration separately from harness recovery, assertions, bounded side effects, transformed evidence hashes, and artifact policy. Semantic validation is applied after JSON Schema validation.

Non-raw evidence is always terminal-sanitized, credential-redacted, rescanned, bounded, and hashed during collection. Raw ANSI remains local by default; exceptional upload requires both a synthetic/no-secret attestation and a successful secret scan and remains explicitly marked unsanitized.

`exploratory-supplement.v1.json` is the additive handoff for the bounded exploratory agent. It is bound to a deterministic results hash, sanitizes advisory text, may request additional optional flows, and cannot contain deterministic status fields or override required outcomes.

Product-flow orchestration, the release-equivalent binary container, and baseline execution land in later TUI-QA-02 stack layers.

## Driver decision

The selected driver is `@microsoft/tui-test@0.0.4` (MIT, published 2026-04-04). Its `node-pty` boundary and `@xterm/headless` cell model provide semantic locators, terminal cells, resize, keyboard, mouse, signal, and raw trace capabilities in one adapter. `node-pty@1.2.0-beta.11` is a direct exact dependency rather than an optional transitive dependency, so installation cannot succeed while silently omitting the PTY backend. Both packages exceed the repository's seven-day minimum release age.

A byte-oriented Go PTY/expect implementation was rejected for this layer because it would require building and maintaining a second terminal emulator or reducing assertions to raw byte matching. Raw bytes remain evidence, but the workbench also needs terminal-cell semantics.

The selected driver currently resolves deprecated `glob@10.5.0` within its declared dependency range. `npm audit` reports no vulnerability; this layer does not force an unvalidated major-version override outside the driver's range.

Node is pinned by `.node-version`; package metadata accepts that pinned Node 24 release line. The lockfile contains exact resolved versions and integrity hashes. `allowScripts` permits only the reviewed exact `@swc/core` binding validator and `node-pty` prebuild validator/post-install cleanup.

## Commands

```sh
npm ci
npm run unit
npm run driver
npm run check
```

From the repository root:

```sh
make tui-qa-check
TUI_QA_CONTAINER_IMAGE=<reviewed-image> tools/tui-qa/run-containment-selftest.sh
```

`npm run check` executes unit tests, production TypeScript compilation, test typechecking, and the real PTY driver smoke. A green typecheck without the PTY smoke is not considered driver validation.

## Restoration evidence

`pty-wrapper` writes `AM_TUI_QA_TARGET_EXIT_BOUNDARY` before any harness reset. Product restoration must be evaluated only from bytes before that boundary and the captured `targetAtExit` termios state. Harness cleanup after the boundary is reported separately as `harnessRecovery`; it cannot satisfy a product restoration assertion.
