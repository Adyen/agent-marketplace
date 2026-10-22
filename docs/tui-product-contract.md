# TUI Product Contract

This document records the `agent-marketplace tui` behavior covered by GitHub issue #41. It is a reference lead for maintainers, not a normative contract: `openspec/specs/` is the normative description of user-observable behavior, including the `tui-navigation`, `tui-rendering`, `tui-interactions`, and `tui-feedback` capabilities. It does not redefine configuration schema, migration, target precedence, or materialization policy; `AGENTS.md`, `openspec/specs/`, and their owning packages govern those. The TUI consumes those contracts and must report their outcomes truthfully.

## Authority and enforcement

Contract sources apply in this order:

1. Repository security and configuration rules.
2. Normative `openspec/specs/` specifications, including the `tui-*` capabilities.
3. Behavioral tests through public TUI, pipeline, and materialization paths.
4. Direct real-terminal QA reports. These are evidence and never define intended behavior by themselves.

Initial baseline discovery is non-blocking. Enforcement may begin only after every Critical or High defect in a required flow is fixed; such defects are never accepted as expected behavior.

Severity follows user impact:

| Severity | Impact |
|---|---|
| Critical | Compromise, destructive corruption, or loss of terminal or protected-data control. |
| High | An inaccessible or misleading core action, confirmation, quit path, or recovery path. |
| Medium | Workflow degradation with a viable workaround. |
| Low | Cosmetic impact that does not impede completion. |

## Asynchronous requests

Replaceable detail rendering and snapshot-producing load, reload, refresh, and update work use centralized request kinds with process-local, monotonically increasing generations. A generation is allocated only when work is scheduled. A completion applies only when its kind and generation match the active request, and an accepted completion cannot be replayed.

Rejected stale completions do not change the active pane, input focus, selected resource, detail content, rows, cached configuration, drift state, or success and error state. An update may already have changed disk before its state delivery becomes stale, so a stale update result schedules a new authoritative snapshot instead of applying either stale outcome.

Accepted snapshot rebuilds restore stable row or section selection and retain the identity of an open detail resource when rows are inserted or reordered; the restored selection keeps its line within the list viewport, so rows inserted above it do not shift it on screen. After disabling a resource, the mutation-triggered rebuild instead selects the next resource that was below it, so the cursor does not follow the disabled resource to its new section.

The startup snapshot is delivered progressively under one request generation. Once the merged configuration validates, the actual locally configured selection set renders immediately as verification-pending rows: explicit selections plus local-directory `all:true` and tag selections expanded through a read-only local catalog ingest (never network fetch or lock access), with configured V4 and personal-local excludes already applied. They show a green check and scope badge for selections enabled in validated config, with an explicit verification-pending status and footer indicator; neither check nor badge claims installation or in-sync state, and they cannot open toggles, installs, or sync until verified inventory replaces them. So that verification does not reshuffle the list, pending rows take the positions and badges verified inventory will give them: optional organization Marketplace resources excluded by the user render unchecked with their Global badge, selected collections render unchecked without a badge (verified inventory lists their members as enabled and the collection itself as available), Marketplace collection pins render no row, registry selections carry their harness-override badge, and an optional collection member yields to another enabled selection of the same resource. A Marketplace collection whose members are not yet pinned — such as an organization collection awaiting transient acquisition — expands from its locally verified cached manifest (the exact configured revision when cached, otherwise the most recently cached one). That read never fetches, its members are pending display only, and a missing or unverifiable cache entry leaves the members to arrive with verified inventory. When an organization Marketplace has no deployed lock, these provisional rows come from the validated pre-acquisition configuration — never from the pin-stripped browse-safe configuration — so configured selections render while transient acquisition is still blocked. Verified inventory then replaces pending rows without duplicates or fabricated badges; a provisional failure leaves recoverable diagnostics and never authorizes mutation. A config stage then publishes it and starts cached-first Marketplace discovery, so cached browsing and search results can render before local catalog verification, detail capture, and the materialization dry run finish. Once exact-lock and catalog resolution complete, an inventory stage publishes the verified rows: they are navigable, searchable, and otherwise-eligible config-only toggles may begin, while drift remains explicitly pending and is never presented as `in sync` or assessed. A final assessed stage then replaces the pending inventory with authoritative rows, drift, and diagnostics. Stages share the snapshot generation, so a superseded or failed stream cannot overwrite newer rows, selection, pane, filters, operation outcome, or drift, and an intermediate stage can never complete a pending mutation reload. An invalid configuration still delivers only one fail-closed final stage. When an organization Marketplace has no deployed lock and must be transiently acquired, the config stage additionally publishes a browse-safe configuration carrying only the well-known-verified endpoint identity — never provisional instance IDs or unverified resource pins — so cached browsing and search render while acquisition is still blocked; acquisition and exact validation remain mandatory before the authoritative inventory, toggles, and pins. Post-write reloads stay single authoritative deliveries. Cached Marketplace pages are read on the discovery command's goroutine, never inside Bubble Tea's `Update`.

### Marketplace discovery

Marketplace discovery is replaceable and non-authoritative.

- The TUI renders pending local selections first and replaces them with verified inventory as resolution completes. Each configured Marketplace may add a first page of at most 50 results to the existing Available group.
- Search filters loaded rows immediately and starts a remote search after approximately 250 milliseconds without input. A new request cancels and supersedes the prior generation.
- Clearing search restores the empty-query browse results.
- Discovery has no pagination and does not change the command-line `search` interface.

The cache isolates records by endpoint and query. Records are fresh for five minutes and may remain stale for up to 24 hours. Stale rows stay visible during revalidation and after an endpoint fails. Each endpoint and its diagnostics update independently. Loading, stale data, truncation, and partial failures must not change snapshot health or drift state.

### Marketplace installation

Discovery rows show their Marketplace and Registry identity, and the TUI sanitizes remote text before rendering it. Blocked and revoked rows stay inspectable but cannot be installed. Search and cache metadata must never authorize installation.

- Installation must use the exact selected locator for Marketplace preparation.
- When the verified plan has security findings, the TUI requires confirmation of its preview. Without findings, it commits that same prepared plan directly.
- Before mutation, the pipeline reloads effective configuration and rejects changed Marketplace or Registry policy, target harnesses, endpoint identity, or collisions.
- It writes exact pins atomically to an explicit user, project, or local scope.
- The mutation operation stays active until the subsequent authoritative snapshot reload succeeds or reports failure.

### Install from source

Pressing `i` in the resource list opens a Source → Pick → Review modal for an unconfigured Git URL, GitHub/GitLab web URL, or directory. A web URL supplies its encoded ref and path. Preview checks the effective registry allowlist and path before network access and performs no config, lock, or harness writes. It shows discovered resources grouped by kind, malformed rows as unselectable, and bundle paths when the source expands into several catalogs.

The user selects kind-qualified resources, chooses a writable user, project, or local scope, and reviews the configuration file and picks before confirming. Git installs carry the previewed commit; when the ref moves, the install is refused with both commits, the modal returns to Pick, and picks still present after re-preview remain selected. When the chosen scope's lock keeps the source at an older commit than the preview, the modal stays on Review with the picks and scope kept and shows the `update` command that advances it, without previewing again. Cancel from any step leaves configuration, acquisition lock, and harness bytes unchanged. The install uses one guarded mutation and an authoritative snapshot reload.

On Source, `↓` and `↑` move focus URL→Ref→Path and back without wrapping, and `tab`/`shift+tab` move between the fields with wrapping; `k` and `j` remain text. `→` acts as enter on Pick and `←` returns one step; `→` on Review never installs, and on Source `←` and `→` move the text cursor. Review moves the scope with `↑`/`↓` (or `k`/`j`) through User, Project, and Local without wrapping; a scope the source cannot be written to keeps the current scope and shows the reason. Confirming Source with the URL, ref, and path of the shown preview returns to Pick with that preview and the current picks, without previewing again. A Git preview without a ref, or pinned to a full commit SHA, reads the cache without fetching when the cached checkout is already at that commit.

### Marketplace row detail

Opening a Marketplace row's detail never resolves a registry-relative path against the consumer workspace.

- A discovery-only skill row that is currently installable (one without local verified content) asynchronously loads its exact revision's verified inspection from the Marketplace endpoint. The primary file renders first; supporting text files are fetched lazily on selection. The inspection verifies the endpoint's instance identity, the manifest's registry, kind, name, revision, and content digest against the discovery item, and re-verifies each file response against the accepted manifest. Browsing performs no installation, configuration, acquisition-lock, or materialization writes. Unavailable, corrupt, unsupported, or identity-mismatched inspection content produces an actionable failure state; binary and other non-text files show identity information instead of bytes. A superseded inspection response cannot replace another resource's open detail.
- Any other discovery-only row (one without local verified content) opens a bounded metadata detail showing its index fields (kind, name, status, Marketplace endpoint, registry, revision, digest) instead of fabricated file content. An installable row ends with an install hint; a blocked, revoked, or otherwise unavailable row reports that it is not currently installable.
- An installed pin whose verified content is missing reports that sync can restore it.
- An installed pin backed by a verified canonical `artifact.tar.gz` opens the same split bundle browser over the archive's members. The sibling verification marker must be present, the archive is size-bounded and re-validated before any member reaches the preview, and missing or corrupt content fails closed.
- An installed skill whose verified supporting files are staged separately from its canonical definition gains one virtual `SKILL.md` member compiled from the skill's attached verified IR; the supporting files keep their filesystem-backed rendering, nothing is written into the staging directory, and missing canonical primary content fails visibly.
- A canonical skill payload is presented as a synthetic `SKILL.md` with YAML frontmatter and its Markdown body; the internal IR JSON is not exposed.

## Mutation operations

Config-changing TUI interactions are serialized. Resource toggles, target saves, and editor sessions carry operation identity in their completion messages. A second mutation is blocked while one is active, and late or duplicated completions cannot clear or overwrite the active operation. Collision follow-up writes keep the primary toggle's operation identity.

A successful resource-toggle write stays active until an authoritative post-write snapshot is accepted, including when a later snapshot supersedes the first reload. An accepted failed snapshot reports an operation failure and releases the operation without presenting the write as a verified success.

## Diagnostics, drift, and update

Snapshot diagnostics and operation diagnostics have separate lifecycles. An accepted snapshot atomically replaces earlier snapshot diagnostics without erasing an operation failure. An operation failure stays visible until a successful operation of the same kind supersedes it. Stable diagnostic identity deduplicates repeated values within one outcome.

Hard snapshot errors make drift unavailable or failed, never `in sync`. Before the assessed stage lands, drift is explicitly pending and shown as unavailable rather than `in sync` or assessed. A configuration parse or load failure may produce no structured rows at all. In that case the E editor remains the recovery path, because row intent cannot be inferred safely.

Hard catalog, resolution, dry-run, or materialization errors keep their diagnostics and failed drift but retain the partial inventory:

- Rows from successfully loaded catalogs stay rendered.
- Each selection explicitly configured on a failed registry gains one unavailable recovery row that preserves its kind, name, registry identity, and scope flags.
- A top-level local include gains a recovery row only when exactly one failed registry identity can own it and no healthy available row already covers it.
- Every otherwise-mutable rendered row can open the scope toggle modal while those errors remain.

Required, malformed, unavailable Marketplace, and serialization restrictions are unchanged. Unavailable rows never fabricate detail content: they perform no source lookup and report that no verified content is available.

Automatic snapshot loads, reloads, and refreshes consume every present valid layer lock as an exact snapshot. A missing layer lock lets the TUI fall back to mutable discovery without creating a lock, and is reported as a warning; this never downgrades an invalid present lock. Mutable movement cannot replace inventory protected by a lock. The TUI holds the exact-cache read lease through catalog discovery and materialization, so garbage collection cannot remove a resolved exact snapshot while it is still in use.

TUI update uses the shared sync pipeline and applies an authoritative post-operation snapshot. Its messages distinguish refresh, materialization, and state-reload failures, and success is shown only when the pipeline and the resulting snapshot are healthy.

## Exit and terminal lifecycle

`q` follows the interactive quit flow. Ctrl+C and external SIGINT/SIGTERM are hard exits. A hard exit does not promise rollback of filesystem work already completed; the next load reports the resulting state.

An outer terminal guard restores captured termios state, disables mouse and bracketed-paste modes, makes the cursor visible, and exits the alternate screen on clean quit, handled signal, startup failure, or returned error. Only Bubble Tea's interrupt sentinel is treated as a clean interrupt. Other returned errors preserve `errors.Is`/`errors.As` identity, while terminal-control and structural bytes are escaped before the caller prints them.

While the alternate screen is active, structured logs continue to the managed log file but never mirror onto the live terminal. An active external editor receives SIGINT/SIGTERM, has a bounded grace period, and is killed and reaped if it does not exit.

## Terminal size and rendering

The supported minimum is 50 columns by 12 rows. At or above that size, the active core view or modal stays within physical bounds and keeps its required action, confirmation, quit, and recovery keys visible. Help and diagnostics are scrollable with persistent offsets.

Below either minimum dimension, the TUI displays only a bounded resize-required state. Normal keyboard workflow input is suspended, while `q` and Ctrl+C stay available. Returning to a supported size restores the prior workflow state.

The TUI enables Bubble Tea cell-motion mouse reporting to receive wheel events. Each vertical wheel-up or wheel-down event scrolls the file preview, help, errors, or Marketplace consent by one line; over the bundle tree, it moves the tree selection by one entry; in the resource list, it moves the selection by one row. An unmodified left press and release in the same cell over a resource-list row selects it; on the already-selected resource it opens the detail view like Enter, and on a section header it folds or unfolds that section. The same click over a bundle-tree file selects it and requests its preview, and over a directory expands or collapses it. A press that moves to another cell before release, a modified or non-left click, and clicks over the header, search row, footer, empty rows, the right-hand pane, an overlay other than the errors body, dialogs, the command palette, while typing a search, or below the minimum size do nothing. An unmodified left-button drag inside the right-hand details or file-preview pane, or within the displayed diagnostics body on the errors screen, highlights the covered cells and copies their displayed text on release. Copied text excludes colors and terminal controls and keeps Unicode intact; on the errors screen, the footer and blank padding are excluded. Copying uses the terminal's OSC52 clipboard mechanism, so reaching the system clipboard requires terminal or multiplexer OSC52 support. A click without a drag copies nothing, and drags starting in the resource list, bundle tree, footer, or an overlay other than the errors body select nothing. Scrolling, resizing, resource or file changes, replacement preview content, and changes to the displayed diagnostics clear an active selection; Escape retires it before its normal action. Horizontal wheel events do not trigger TUI actions; Shift+vertical-wheel remains vertical. During a resource-list wheel burst, every accepted event still applies in order while presentation is paced to at most one frame per 60 Hz interval; a trailing frame renders the final selection after input stops, and keys, resizes, asynchronous state, and pane changes refresh immediately and invalidate pending frames. Because the terminal reports mouse events to the application, native click-and-drag text selection may require a terminal-specific modifier (often Option or Shift) while the TUI is active.

Registry metadata, paths, diagnostics, config values, Markdown-derived text, and returned errors are untrusted terminal data. C0, DEL, C1, ESC, carriage return, tab, unsupported structural newlines, invalid UTF-8, Unicode line separators, and bidi formatting controls render as visible escapes. Markdown is sanitized before entity decoding can turn encoded values into terminal controls. Printable Unicode, including combining and double-width characters, stays readable.

### Bundle browser

The full resource view is a split bundle browser. The left pane is an expandable, deterministic relative-path tree, and the right pane previews the selected file.

- Directory entries precede files, and each group is sorted lexically.
- `j`/`k` and the arrow keys move through visible entries. `h`/left folds, `l`/right expands, and enter toggles directories (enter on a file keeps it selected).
- `Esc`/`q` returns to the resource list, while Ctrl+C remains the hard exit.
- Directory-backed skills select `SKILL.md` initially when present.
- File-backed resources use the same shell with one file entry and keep their JSON/Markdown/text rendering behavior.

### Read and render limits

- Bundle source reads are capped at 256 KiB per file. Only regular files beneath the resolved resource root can be previewed; symlinked and special entries are omitted and traversal is rejected.
- Detail source reads are capped at 256 KiB per entry and, across one accepted snapshot refresh, at 2 MiB and 64 entries. They are copied while the exact-cache lease is held, so deferred opens and width re-renders stay valid after cache GC without retaining a session-long lease. Details beyond the aggregate byte or entry budget are visibly marked omitted, and partial reads are visibly marked truncated.
- Unbroken lines are split before rendering, and rendered output is capped at 5,000 lines with visible truncation markers.
- Markdown renderers are synchronized, keyed by viewport width, and bounded to recent widths.
- Preview requests carry both a generation and the selected relative path, so stale completions cannot replace the current selection. A width change re-renders the selected file under request-generation control while preserving its scroll anchor.
- The browser stays within the 50-column by 12-row minimum. At narrow supported widths, its footer keeps the back and hard-quit actions visible.

## Performance evidence

Changed user-visible render and state paths keep their benchmarks. Progressive startup reports milestones separately, with allocation data on identical fixtures: config validation as a prerequisite (`config_ready_prerequisite`), the real startup path from staged config read through the config stage and the cached-first discovery it launches until the first cached Marketplace row is rendered (`first_visible`), first toggle-ready verified inventory (`toggle_ready`), and the fully assessed snapshot (`fully_assessed`). Base and head comparisons use identical selections where a base equivalent exists; genuinely new paths record a repeated HEAD baseline with allocation data.
