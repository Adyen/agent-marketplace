# TUI Product Contract

This document defines the `agent-marketplace tui` behavior covered by GitHub issue #41. It does not redefine configuration schema, migration, target precedence, or materialization policy. Those remain governed by `AGENTS.md`, `docs/PRECEDENCE.md`, and their owning packages. The TUI consumes those contracts and must report their outcomes truthfully.

## Authority and enforcement

Contract sources apply in this order:

1. Repository security and configuration rules.
2. Current normative product documentation, including this contract.
3. Behavioral tests through public TUI, pipeline, and materialization paths.
4. QA captures and snapshots, which are evidence and never define intended behavior by themselves.

Initial baseline discovery is non-blocking. Enforcement may begin only after every Critical or High defect in a required flow is fixed; such defects are never accepted as expected snapshots.

Severity follows user impact:

- **Critical**: compromise, destructive corruption, or loss of terminal or protected-data control.
- **High**: an inaccessible or misleading core action, confirmation, quit path, or recovery path.
- **Medium**: workflow degradation with a viable workaround.
- **Low**: cosmetic impact that does not impede completion.

## Asynchronous requests

Replaceable detail rendering and snapshot-producing load, reload, refresh, and update work use centralized request kinds with process-local, monotonically increasing generations. A generation is allocated only when work is scheduled. A completion applies only when its kind and generation match the active request, and an accepted completion cannot be replayed.

Rejected stale completions do not change the active pane, input focus, selected resource, detail content, rows, cached configuration, drift state, or success and error state. Because an update may already have changed disk before its state delivery becomes stale, a stale update result schedules a new authoritative snapshot instead of applying either stale outcome.

Accepted snapshot rebuilds restore stable row or section selection and retain the identity of an open detail resource when rows are inserted or reordered.

## Mutation operations

Config-changing TUI interactions are serialized. Resource toggles, target saves, and editor sessions carry operation identity in their completion messages. A second mutation is blocked while one is active, and late or duplicated completions cannot clear or overwrite the active operation. Collision follow-up writes retain the primary toggle's operation identity.

## Diagnostics, drift, and update

Snapshot diagnostics and operation diagnostics have separate lifecycles. An accepted snapshot atomically replaces earlier snapshot diagnostics without erasing an operation failure. An operation failure remains visible until a successful operation of the same kind supersedes it. Stable diagnostic identity deduplicates repeated values within one outcome.

Hard configuration, catalog, resolution, dry-run, or materialization errors make drift unavailable or failed, never `in sync`. Rows produced by a hard-failed snapshot remain inspectable but cannot initiate config mutation until a healthy snapshot is accepted.

TUI update uses the shared sync pipeline and applies an authoritative post-operation snapshot. Copy distinguishes refresh, materialization, and state-reload failures; success is shown only when the pipeline and resulting snapshot are healthy.

## Exit and terminal lifecycle

`q` follows the interactive quit flow. Ctrl+C and external SIGINT/SIGTERM are hard exits. A hard exit does not promise rollback of filesystem work already completed; the next load reports the resulting state.

An outer terminal guard restores captured termios state, disables mouse and bracketed-paste modes, makes the cursor visible, and exits the alternate screen on clean quit, handled signal, startup failure, or returned error. Only Bubble Tea's interrupt sentinel is treated as a clean interrupt. Other returned errors preserve `errors.Is`/`errors.As` identity while terminal-control and structural bytes are escaped before the caller prints them.

While the alternate screen is active, structured logs continue to the managed log file but never mirror onto the live terminal. An active external editor receives SIGINT/SIGTERM, has a bounded grace period, and is killed and reaped if it does not exit.

## Terminal size and rendering

The supported minimum is **50 columns by 12 rows**. At or above that size, the active core view or modal stays within physical bounds and keeps its required action, confirmation, quit, and recovery keys visible. Help and diagnostics are scrollable with persistent offsets.

Below either minimum dimension, the TUI displays only a bounded resize-required state. Normal keyboard and mouse workflow input is suspended; `q` and Ctrl+C remain available. Returning to a supported size restores the prior workflow state.

Registry metadata, paths, diagnostics, config values, Markdown-derived text, and returned errors are untrusted terminal data. C0, DEL, C1, ESC, carriage return, tab, unsupported structural newlines, invalid UTF-8, Unicode line separators, and bidi formatting controls render as visible escapes. Markdown is sanitized before entity decoding can turn encoded values into terminal controls. Printable Unicode, including combining and double-width characters, remains readable.

Detail source reads are capped at 256 KiB, unbroken lines are split before rendering, and rendered output is capped at 5,000 lines with visible truncation markers. Markdown renderers are synchronized, keyed by viewport width, and bounded to recent widths. A width change re-renders the open detail under request-generation control while preserving its scroll anchor.

## Performance evidence

Changed user-visible render and state paths retain benchmarks. Base and head comparisons use identical selections where a base equivalent exists; genuinely new paths record a repeated HEAD baseline with allocation data.
