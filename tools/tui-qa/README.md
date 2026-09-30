# TUI Real-Usage Tests

This package uses `@microsoft/tui-test` to launch the real `agent-marketplace` binary in a PTY and operate it. Each journey file sends the keys a user would press and checks what appears on screen, plus the resulting files on disk.

## Test ownership

- Go tests own TUI model state, asynchronous behavior, rendering contracts, and terminal-guard details.
- Go E2E tests own the configuration, resource, scope, collision, ownership, harness-target, and materialization matrices.
- This package owns direct user interaction through a real PTY.

## Journeys

Each interaction contract has its own file. The `*.spec.ts` files are PTY journeys run by `tui-test`; the `*.test.ts` files run under `node --test`.

1. `tests/launch-browse-inspect.spec.ts` launches with no arguments, browses all tabs, opens details, returns, and quits.
2. `tests/search-and-select.spec.ts` searches, checks that the match stays and a distractor disappears, and selects the match.
3. `tests/local-toggle-and-sync.spec.ts` toggles an available skill locally, observes pending drift, syncs, and verifies the config and the files written to the harness folders.
4. `tests/help-and-return.spec.ts` opens help and returns to the same selection.
5. `tests/resize-and-recover.spec.ts` shrinks the terminal below the minimum size, checks that a toggle is blocked, and recovers at exactly 50x12.
6. `tests/ctrl-c-hard-exit.spec.ts` exits with Ctrl+C within a time bound.
7. `tests/sigterm-hard-exit.spec.ts` sends SIGTERM to the target PID that the launcher published and verifies the process is gone.
8. `tests/non-tty.test.ts` checks the two non-TTY entry points in a process sandbox with no registry.
9. `tests/org-marketplace-lock-failure.spec.ts` starts cold with an organization v4 Marketplace declaration whose lock is missing. It verifies that TUI startup, a warm selection edit, sync, and update all reuse the exact resource in memory without writing organization policy. It enforces time budgets for navigation, write acknowledgement, and settled state while the stub delays acquisition. It then shows that CLI sync, and a full wipe of user-side state followed by re-initialization, recover the same way.
10. `tests/org-marketplace-lock-recovery.spec.ts` runs the same exact organization Marketplace resource with its lock published, and verifies the settled TUI baseline, the status groups, and a passing drift check.
11. `tests/reinstall-recovery.test.ts` deletes every personal and generated layer, re-initializes, syncs back to a healthy workspace, and verifies that plain sync recreates a missing repository lock.
12. `tests/status-disable-scroll.spec.ts` disables an enabled resource in an overflowing Status list and verifies that the adjacent row and its section header stay visible.
13. `tests/accept-user-migration.spec.ts` accepts the startup migration prompt. It verifies that only the legacy user root converts (with a byte-exact backup) and that the committed legacy repository root is untouched. It then selects a repository-declared Git resource globally into the migrated user root, syncs, and passes a drift check.
14. `tests/v4-directory-global-toggle.spec.ts` starts on a v4 user config with a local directory source, enables a skill globally, syncs it into the Factory, Gemini, and Devin user roots, disables it, syncs again, and passes `sync --check` without creating any lockfile.
15. `tests/v4-directory-error-visibility.spec.ts` starts on the same v4 directory setup, makes the user config invalid while the toggle modal is open, and verifies that the failed toggle keeps its error visible, the footer keeps its recovery actions at 50x12, and the full diagnostic opens without the config being rewritten.
16. `tests/layout-quality.spec.ts` checks that the footer keeps the command palette, help and quit hints, that the toggle and edit-config modals render as one complete frame that keeps file names visible, and that the footer and palette still fit at 50x12.

`tests/target-pid.test.ts` is a helper test. It checks that the PID lookup used by the SIGTERM journey waits for the launcher to publish the PID.

The Git-backed PTY journeys start on a legacy configuration and see the startup migration prompt. All of them except the acceptance journey decline it. The two v4 directory journeys start on a v4 user root and get no prompt.

Lifecycle hooks give every PTY journey a fresh temporary HOME, XDG roots, workspace, and temporary directory. Git-backed journeys also get a local bare Git registry with a project-selected, pre-synced `beta-skill` and an available `alpha-skill`. The v4 directory journeys read the same two skills from a plain directory, and the fixture's initial sync selects neither of them. Test discovery does not create fixtures. The test binary uses the existing `e2e` build tag only to redirect the fixed organization-config path into the fixture.

The two organization journeys also install a v4 organization layer at `<root>/etc/agent-marketplace` that declares one Marketplace source. A loopback stub endpoint backs it and serves the exact registry, revision, artifact, well-known identity document, and an empty search page. The lock-recovery fixture publishes the matching v2 organization lock. The lock-failure fixture omits that lock and skips the fixture pre-sync. The cold TUI must then acquire verified evidence in memory, reuse it through a selection edit, refresh, sync, and update, write the resource into the harness folders, and never write organization policy. The Status-overflow journey adds thirty selected fixture skills so the list scrolls for real instead of showing two rows.

Every state-changing input is followed by a visible assertion caused by that input before the next input is sent. Ordinary UI and process waits are bounded at five seconds, and Git and materialization waits at thirty seconds. The lock-failure journey also enforces 1 second for local navigation, 3.5 seconds for write acknowledgement, and 4.5 seconds for a warm authoritative toggle. These are latency ceilings, not hang timeouts. Three local baseline runs measured about 50 ms for navigation and 2.2 seconds for the settled toggle. With a three-second injected acquisition delay, the toggle took over five seconds before the fix in #302, while the fixed code stays below the ceiling. The CLI reacquisition after quitting shows that the delayed-server control was active. The suite runs with one worker and zero retries, and its timeouts are the same locally and in CI.

## Commands

The package pins pnpm 11.20.0 and Node.js `>=24.18.0 <25` in `package.json`; `make tui-qa-check` refuses any other pnpm version. Run from the repository root:

```sh
pnpm --dir tools/tui-qa install --frozen-lockfile
make tui-qa-check
make tui-qa
```

`make tui-qa-check` typechecks the journeys and confirms that the `tui-test` runner loads. `make tui-qa` builds the `e2e`-tagged test binary, runs the `node --test` files, and then runs every PTY journey once. A failure prints the test runner's console report and the current terminal screen; the suite does not write snapshots or traces.

`make tui-qa` always builds its own binary and sets `AM_TUI_QA_BINARY` and `AM_TUI_QA_REPO_ROOT` itself. To test a binary you already built, run `pnpm qa` (or `pnpm qa:compiled` after `pnpm build`) in `tools/tui-qa` with `AM_TUI_QA_BINARY` set to that binary and `AM_TUI_QA_REPO_ROOT` set to its repository root. The default suite needs no direnv and no organization-specific image, certificate, registry, or infrastructure path.
