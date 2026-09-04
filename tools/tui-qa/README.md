# TUI Real-Usage Tests

This package uses `@microsoft/tui-test` to launch and operate the real `agent-marketplace` binary in a PTY. Scenario files send the same keys a user sends and assert visible behavior plus the relevant filesystem outcome.

## Test ownership

- Go tests own TUI model state, asynchronous behavior, rendering contracts, and terminal-guard details.
- Go E2E tests own configuration, resource, scope, collision, ownership, harness-target, and materialization matrices.
- This package owns direct user interaction: navigation, search, help, resize, quit/signals, and one local-toggle-to-sync journey.

## Journeys

`tests/terminal-core.spec.ts` covers four real-PTY journeys:

1. bare 80x24 launch, tabs, search, help, resize, and `q`;
2. explicit 50x12 launch and Ctrl+C;
3. active SIGTERM;
4. local resource toggle and sync with exact config and output assertions.

`tests/non-tty.test.ts` covers the fifth journey: bare non-TTY help and prompt explicit-TUI failure.

Every journey receives a fresh temporary HOME, XDG roots, workspace, and local bare Git registry. The test binary uses the existing `e2e` build tag only to redirect the fixed organization-config path into that fixture.

## Commands

Run from the repository root:

```sh
pnpm --dir tools/tui-qa install --frozen-lockfile
make tui-qa-check
make tui-qa
```

`make tui-qa-check` typechecks the direct tests and verifies the PTY adapter is installed. `make tui-qa` builds the test binary and runs all five journeys once with one worker and no retry.

## Artifacts and privacy

Sanitized snapshots are written below `.local/agent-marketplace/qa/tui/<run-id>/snapshots`. Raw PTY traces stay under the ignored artifact root and are not uploaded by CI.

Optional local values may be exported directly or loaded from `.envrc.local`, which is gitignored:

- `AM_TUI_QA_RUN_ID` selects the artifact subdirectory.
- `AM_TUI_QA_ARTIFACT_DIR` overrides the artifact root.
- `AM_TUI_QA_BINARY` selects an already-built test binary.
- `AM_TUI_QA_PRIVATE_VALUES` is a newline-separated list that must not appear in tracked QA files or written snapshots.

The default suite requires no organization-specific image, certificate, registry, or infrastructure path.
