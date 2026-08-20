# TUI Product Contract

This document is the normative product contract for the `agent-marketplace tui` behavior covered by GitHub issue #41 under parent epic #40.

## Authority and approval

Contract sources apply in this order:

1. Repository security policy, strict v3 mutation/migration rules (with v2 accepted as migration input), and the authoritative harness target registry.
2. This product contract and the current selection/precedence reference.
3. Behavioral tests that exercise the public TUI, configuration, pipeline, and materialization paths.
4. QA captures and snapshots, which are evidence only and never define intended behavior by themselves.

Repository maintainers own this contract and its exceptions. A change to TUI QA contracts or enforcement requires approval from at least one repository maintainer. The same authority must approve a temporary exception; an exception must identify its scope and residual impact and must not weaken strict configuration validation, security controls, logging policy, or benchmark policy.

## Baseline and enforcement policy

Initial baseline discovery for issue #41 is non-blocking: discovery may report existing product defects while contract work is in progress. Enforcement may start only after every Critical or High defect in a required flow is fixed. Such defects are never accepted as expected snapshots.

Severity is based on user impact, not pixel difference:

- **Critical**: compromise, destructive corruption, or loss of control over the terminal or protected data.
- **High**: a core action, confirmation, quit path, or recovery path is inaccessible in a supported terminal.
- **Medium**: workflow degradation or clipping has a viable documented workaround.
- **Low**: cosmetic impact does not impede the workflow.

## Target harness configuration

The effective target harness set follows project-first precedence:

1. `<repo>/.agents/config.yaml` is authoritative for that project when it declares a non-empty target set.
2. `~/.agents/config.yaml` applies when the project does not declare one.
3. `/etc/agent-marketplace/config.yaml` is the read-only organization fallback.
4. Repository-local `config.local.yaml` must not declare top-level `harnesses`.

The harness modal can edit the user-personal or committed project file. It displays the effective target set, both writable scopes, and the effective source. Saving while organization fallback is effective creates an override in the selected writable scope; it never rewrites the organization file.

First-run initialization opens only when project, user-personal, and organization configuration provide no effective target set. It writes a minimal strict v3 user-personal config. An empty or unknown selection fails before file creation or mutation.

Target choices, labels, validation, ordering, and installation-root detection come from the authoritative target registry. Every UI-enabled compiler target is offered in declared order: Factory Droid, Claude Code, Cursor, Windsurf, Gemini CLI, Devin CLI, then Codex. Devin is detected from `~/.config/devin` and Codex from its resolved Codex home; both remain explicit opt-in choices rather than implicit defaults.

V2 migration emits strict v3, preserves comments, registries, selectors, and excludes, moves a legacy repository target set to user scope when needed, removes legacy repository/local top-level target values, validates every output before writing, and executes through a locked, source-digest-checked commit plan.

## Asynchronous state

Every replaceable detail, reload, or refresh request has a monotonically increasing generation. A completion is accepted only when its kind and generation match the currently active request. Rejected stale results do not change the pane, focus, selection, rows, drift assessment, or diagnostics.

## Diagnostics and drift

The TUI uses one typed diagnostic model with severity, provenance, category, operation identity, and a stable key when the source exposes one.

- An accepted snapshot generation atomically replaces diagnostics from the previous snapshot generation.
- An operation failure remains until a success of the same operation type or explicit acknowledgement in the diagnostics view.
- Exact stable identities deduplicate within one generation or operation.
- Independent operation attempts retain distinct operation identities and are not collapsed together.
- Closing the diagnostics view acknowledges retained operation diagnostics only when every operation diagnostic has been visible: the diagnostics viewport must have reached the bottom (content that fits entirely qualifies). Closing a truncated diagnostics pane before reaching the bottom never acknowledges unseen operation diagnostics; the footer states the live rule (`esc acknowledge` only at the bottom). Current snapshot diagnostics are never acknowledged by closing; they are replaced by the next accepted snapshot.

Materialization dry-run errors make drift unavailable or failed, never `in sync`. Error diagnostics from a partial report remain visible. Orphaned output is represented even when no resolved catalog row exists and includes output identity sufficient to investigate it. Startup failures before drift computation render drift as unavailable rather than treating a zero-value report as clean.

Update copy distinguishes cache/update work from materialization. Success is reported only after both refresh and materialization succeed and the resulting snapshot is accepted. A partial or failed mutation never emits its corresponding success message.

## Exit and recovery

Exit behavior during active work is:

- `q` follows the interactive quit flow. Pending drift requires the sync-and-quit confirmation; choosing sync waits for that sync result, while choosing quit without sync exits without rollback.
- Ctrl+C as terminal input and external SIGINT are hard exits.
- SIGTERM follows the same hard-exit and terminal-restoration contract. Cancellable long-running operations are future work.
- Bubble Tea restores the cursor, input mode, mouse state, and alternate screen on every handled exit. A returned non-interrupt program error is propagated to the caller after restoration.

A hard exit does not promise rollback or cancellation of filesystem work already in progress. Atomic lower-level writes retain their own guarantees; any completed or partial externally visible state is rediscovered and reported on the next status, reload, or sync.

Recoverable configuration, registry, cache, diagnostic, or materialization failures clear according to the diagnostic lifecycle after the underlying condition is repaired and the relevant operation succeeds. Navigation and state reporting then resume without replaying stale results or duplicating diagnostics.

## Terminal size and rendering safety

The supported minimum terminal is **50 columns by 12 rows**. At or above that size, the active pane or modal fits within the physical frame and keeps its required action, confirmation, quit, and recovery keys visible. Resizing within the supported range preserves the active pane, focus, and selected item.

The help and diagnostics overlays are wrapped, vertically scrollable viewports at every supported size: long content wraps at the frame width instead of hard-clipping, scroll keys (`↑/↓`/`j`/`k`, `pgup/pgdn`, `g`/`G`, mouse wheel) are visible in the overlay footer, and scroll offsets persist across frames, resizes (bottom-anchored viewports stay at the bottom), and close/reopen. In the compact footer layout, whenever error diagnostics or unresolved/failed drift exist, the recovery actions `e:errors · r:reload · q:quit` displace the discovery actions so the path out of a broken state is always visible.

The full detail view renders Markdown at the actual viewport width through a concurrency-safe, width-keyed renderer cache (bounded, least-recently-used eviction) and re-renders with a new generation when the width changes while the pane is open, keeping the scroll anchor (same line offset, clamped; bottom stays bottom). A render completion whose recorded width no longer matches the terminal width at acceptance time is rejected and re-requested at the current width, so the accepted render width always matches what is on screen. Detail source reads are capped at 256 KiB and rendered output at 5000 lines; both caps surface a visible `[truncated: …]` marker, and unbroken source lines longer than 1024 characters are hard-split before rendering because the renderer's word wrap is superlinear in unbroken-token length.

Below either minimum dimension, the TUI renders only a bounded resize-required state with visible resize and quit guidance when the frame can contain it. Normal workflow input is suspended except for hard quit input. Returning to a supported size restores the prior pane, focus, selection, and workflow state.

Registry metadata, config values, paths, Markdown-derived text, and diagnostics are untrusted terminal data. Before those values enter a terminal frame:

- C0, DEL, C1, ESC, carriage return, tab, and unsupported structural newline characters use readable escapes such as `\x1B`, `\x0D`, or `\x85`.
- Bidirectional formatting controls use visible Unicode escapes such as `\u202E`.
- Single-line fields cannot create additional rows or trusted chrome.
- Legitimate Markdown line structure is preserved only in the Markdown body boundary; terminal controls inside it are still escaped before rendering.
- Printable Unicode, including combining and double-width characters, remains readable and is measured by physical display width.

Headers, tabs, modal titles, prompts, outcome indicators, and locator anchors come only from trusted program constants plus sanitized data fields. Text in a data region is never accepted as proof that an operation succeeded.

## Performance evidence

Every modified user-visible render, navigation, snapshot, configuration, resolution, discovery, or materialization hot path retains a benchmark. Base and head runs use identical benchmark selections with `-benchtime 2s -count 5 -benchmem`; comparisons use the repository-pinned `benchstat`. A significant latency or allocation regression blocks acceptance until explained and approved.
