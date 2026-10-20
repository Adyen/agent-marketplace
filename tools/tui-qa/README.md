# TUI Real-Usage Tests

This package uses the `@microsoft/tui-test` library to launch the real `agent-marketplace` binary in a PTY and operate it. Each journey file sends the keys a user would press and checks what appears on screen, plus the resulting files on disk.

## Test ownership

- Go tests own TUI model state, asynchronous behavior, rendering contracts, and terminal-guard details.
- Go E2E tests own the configuration, resource, scope, collision, ownership, harness-target, and materialization matrices.
- This package owns direct user interaction through a real PTY.

## Every TUI change adds or extends a journey

Go tests do not prove that a user's key presses reach the changed behavior, and TUI changes merged without a journey have broken user flows. A pull request that changes non-test files under `internal/tui/` or `cmd/agent-marketplace/tui*.go` must add or modify a `tests/*.spec.ts` or `tests/*.test.ts` file here. `specaudit gate` in the `Specs` CI job enforces this. Changing only `fixture.ts`, `helpers.ts`, or this README does not count, and neither does deleting a journey. Keep journeys directly in `tests/`: `pnpm qa` runs only `dist/tests/*.spec.js` and `dist/tests/*.test.js`, so a journey in a subdirectory never runs and does not count.

1. Extend the journey below that already reaches the changed flow, or add `tests/<flow>.spec.ts` for a new interaction.
2. Drive the flow as a user would, following the timing rules at the end of the Journeys section: settle first, then assert the visible result of each state-changing input before sending the next one. When the flow writes configuration or harness files, check those files too.
3. For a bug fix, confirm the journey fails without the fix.
4. Add or update its entry in the list below, then run `make tui-qa`.

Only a change with no user-observable TUI effect, such as comments, renames, or dead code, may carry a `TUI-QA-Impact: none — <reason>` commit trailer instead. It does not replace `Spec-Impact`, and "covered by Go tests" is not a reason.

## Journeys

Each interaction contract has its own file. The `*.spec.ts` files are PTY journeys and the `*.test.ts` files are process tests; both run under `node --test`. Each journey opens its own in-process `tui-test` session and starts the program with exactly the fixture's environment.

1. `tests/launch-browse-inspect.spec.ts` launches with no arguments, browses the single status view, opens details, returns, and quits.
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
12. `tests/status-disable-scroll.spec.ts` disables an enabled resource in an overflowing status list and verifies that the adjacent row and its section header stay visible.
13. `tests/v4-global-selection.spec.ts` retains the global-selection journey on supported v4 startup. It selects a repository-declared Git resource globally, syncs, passes a drift check, and proves the committed repository root stays byte-identical without conversion or backups.
14. `tests/v4-directory-global-toggle.spec.ts` starts on a v4 user config with a local directory source, enables a skill globally, syncs it into the Factory, Gemini, and Devin user roots, disables it, syncs again, and passes `sync --check` without creating any lockfile.
15. `tests/v4-directory-error-visibility.spec.ts` starts on the same v4 directory setup, makes the user config invalid while the toggle modal is open, and verifies that the failed toggle keeps its error visible, the footer keeps its recovery actions at 50x12, and the full diagnostic opens without the config being rewritten.
16. `tests/marketplace-archive-refresh.spec.ts` starts on a v4 user root declaring a Marketplace source whose lock pins the stub's original archive digest. The endpoint then serves a repaired archive digest for the same content digest, and the journey verifies that an update refreshes the user lock, reports a visible `marketplace.archive-evidence-refreshed` warning in the diagnostics overlay, leaves the materialized skill unchanged, and that a healthy repeat update emits no warning or lock write.
17. `tests/layout-quality.spec.ts` checks that the footer keeps the command palette, help and quit hints, that the toggle and edit-config modals render as one complete frame that keeps file names visible, and that the footer and palette still fit at 50x12.
18. `tests/wheel-burst.spec.ts` writes complete encoded wheel bursts to the PTY, including a direction reversal and a burst that reverses at the first list boundary on the overflowing status list, and verifies the terminal shows the independently expected selected resource and its details after each burst.
19. `tests/preview-selection.spec.ts` drags across the right-hand details pane, checks inverse cells and the exact OSC52 payload, and verifies that scrolling clears the highlight while clicks and list-side drags copy nothing.
20. `tests/marketplace-full-view.spec.ts` searches for an uninstalled Marketplace skill answered only by the stub's query-matched search, opens its full view, and verifies the verified inspection loads the primary `SKILL.md` and a lazily fetched support file. It then drags across the preview text, checks the inverse cells and the exact OSC52 payload, and verifies that wheel input still routes and clears the highlight.
21. `tests/source-kind-bulk-disable.spec.ts` opens View settings with `v`, previews and accepts a Source → Kind grouping, verifies the persisted `tui.json` preferences file, folds the source section, bulk-disables it at project scope through the confirm step, and checks the written exclude plus the authoritative `~ 1 out of sync` drift assessment before quitting. A second launch verifies the accepted grouping survives restart.
22. `tests/click-navigation.spec.ts` sends encoded press and release input over a resource row on the overflowing status list. It verifies that a drag starting on the row changes nothing, that a click selects the row and shows its details, that a second click opens its detail view, and that no click writes to the clipboard.
23. `tests/unsupported-consumer-config.spec.ts` opens an unsupported user root above a previously synced v4 repository through both bare-TTY and explicit-TUI launch, shows the coded file/field diagnostic without a conversion offer, blocks sync/update/harness edits, quits, and verifies configuration, lock and installed resource bytes and metadata stay unchanged.
24. `tests/missing-exact-snapshot-recovery.spec.ts` starts with a valid repository acquisition lock but no exact snapshot in the consumer cache. It verifies a warning with explicit sync guidance, unverified drift, and the sync shortcut, then presses `s` and checks that the warning clears, the locked resource is materialized, and configuration and acquisition-lock bytes remain unchanged.
25. `tests/marketplace-archive-integrity.spec.ts` starts cold on the organization Marketplace while the stub serves a valid placeholder archive whose bytes do not hash to the archive digest. It verifies that the TUI reports the rejected acquisition with its archive diagnostic, installs nothing, writes no organization lock, and does not retry through the legacy final-artifact route.
26. `tests/first-run-setup.spec.ts` launches with no user config. It verifies that harness setup opens with the detected Claude Code pre-selected and that cancelling it writes nothing. A second launch reopens setup, adds Factory Droid, switches to the compact layout at 50x12 and back with the cursor and selection kept, saves the exact v4 user config, and syncs the repository selection into both harness folders. A third launch skips setup and passes `sync --check`.

`tests/target-pid.test.ts` is a helper test. It checks that the PID lookup used by the SIGTERM journey waits for the launcher to publish the PID.

All normal PTY journeys start on exact-v4 consumer roots. No startup conversion prompt is offered. The unsupported-config journey checks the visible format refusal and blocked operations while preserving existing configuration, lock and harness bytes.

Lifecycle hooks give every PTY journey a fresh temporary HOME, XDG roots, workspace, and temporary directory. Git-backed journeys also get a local bare Git registry with a project-selected, pre-synced `beta-skill` and an available `alpha-skill`. The v4 directory journeys read the same two skills from a plain directory, and the fixture's initial sync selects neither of them. The first-run fixture writes no user config, creates `~/.claude` so Claude Code is detected as installed, and skips the initial sync. Test discovery does not create fixtures. The test binary uses the existing `e2e` build tag only to redirect the fixed organization-config path into the fixture.

The two organization journeys also install a v4 organization layer at `<root>/etc/agent-marketplace` that declares one Marketplace source. A loopback stub endpoint backs it and serves the exact registry, revision, well-known identity document, both forms of the archive (the placeholder form at `/api/v1/archives/sha256/<hex>` and the final form at `/api/v1/artifacts/sha256/<hex>`), and a search page that answers a matching query with the never-installed `pack-notes` skill plus its exact-revision inspection manifest and file bodies. The stub compresses its archives with Node's zlib, which no Go toolchain reproduces, so a client that verifies by re-encoding fails under every Go version. The Marketplace journeys assert that the client fetched the placeholder form and never needed the legacy final-artifact route, and the archive-integrity journey proves that the client hashes those bytes by rejecting a placeholder archive that does not hash to its digest. The lock-recovery fixture publishes the matching v2 organization lock. The lock-failure fixture omits that lock and skips the fixture pre-sync. The cold TUI must then acquire verified evidence in memory, reuse it through a selection edit, refresh, sync, and update, write the resource into the harness folders, and never write organization policy. The Status-overflow journey adds thirty selected fixture skills so the list scrolls for real instead of showing two rows.

Every journey waits for the settled drift state (for example `~ in sync`) before its first input, because the TUI can discard input that arrives while it is still verifying resources. Every state-changing input is followed by a visible assertion caused by that input before the next input is sent. Ordinary UI and process waits are bounded at five seconds, and Git and materialization waits at thirty seconds. The lock-failure journey also enforces 1 second for local navigation, 3.5 seconds for write acknowledgement, and 4.5 seconds for a warm authoritative toggle. These are latency ceilings, not hang timeouts. Three local baseline runs measured about 50 ms for navigation and 2.2 seconds for the settled toggle. With a three-second injected acquisition delay, the toggle took over five seconds before the fix in #302, while the fixed code stays below the ceiling. The CLI reacquisition after quitting shows that the delayed-server control was active. The journeys run one file at a time with no retries and a 60-second limit per journey, and their timeouts are the same locally and in CI.

## Commands

The package pins pnpm 11.20.0 and Node.js `>=24.18.0 <25` in `package.json`; `make tui-qa-check` refuses any other pnpm version. Run from the repository root:

```sh
pnpm --dir tools/tui-qa install --frozen-lockfile
make tui-qa-check
make tui-qa
```

`make tui-qa-check` typechecks the journeys and confirms that the native `tui-test` engine loads. `make tui-qa` builds the `e2e`-tagged test binary, runs the process tests, and then runs every PTY journey once. A failure prints the failed step, the assertion and the current terminal screen.

`make tui-qa` always builds its own binary and sets `AM_TUI_QA_BINARY` and `AM_TUI_QA_REPO_ROOT` itself. To test a binary you already built, run `pnpm qa` (or `pnpm qa:compiled` after `pnpm build`) in `tools/tui-qa` with `AM_TUI_QA_BINARY` set to that binary and `AM_TUI_QA_REPO_ROOT` set to its repository root. The default suite needs no direnv and no organization-specific image, certificate, registry, or infrastructure path.

## Failure recordings

Every journey records its terminal as a GIF and writes a `tui-test` trace when its session closes. A passing journey deletes both without rendering the GIF. A failing journey keeps them in `test-results/<journey>/`, or under `AM_TUI_QA_ARTIFACTS` when that is set:

- `<journey>.gif`, the recording up to the failure;
- `trace-*/trace.html` to review in a browser, `trace-*/trace.md` for the failed operation, recent operations and screens, and `trace-*/session.cast` to replay the session in an asciinema player.

On a pull request, each failing `TUI QA` platform uploads its kept journeys as the `tui-qa-failures-<platform>` artifact, and every GIF is also uploaded unzipped on its own. The `TUI QA recordings comment` job then posts or updates a single pull request comment that links each failed journey's GIF and its platform's evidence bundle, and updates it again once TUI QA passes. Pull requests from forks get the same table in the job summary instead. The fixtures hold no credentials, but the trace keeps program arguments and terminal input, so do not add secrets to a journey.
