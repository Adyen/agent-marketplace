# TUI Resource Bundle Browser Design

**Date:** 2026-09-04<br>
**Status:** Ready for implementation dispatch<br>
**Scope:** TUI browsing of files bundled with a resource, especially directory-backed skills

## Summary

The TUI exposes the structure of a resource bundle instead of presenting a directory as one concatenated document. Entering a resource's full view opens a full-screen, two-pane bundle browser: a navigable filesystem tree on the left and a preview of the selected file on the right.

The implementation should use Go's standard filesystem APIs (`os.ReadDir` and `io/fs`) together with the existing Bubble Tea, Bubbles, Lipgloss, and Glamour stack. It should not invoke external programs such as `tree`, `find`, `less`, or a system file manager. This preserves the static-binary design, avoids optional runtime dependencies, and gives the TUI a stable mapping between visible tree entries and source files.

The existing detail renderer remains responsible for reading, sanitizing, formatting, and rendering file content. The primary change is that the browser selects one file at a time instead of eagerly concatenating every file in the bundle. The current source-size, line-length, rendered-output, terminal-safety, and asynchronous request guarantees remain in force.

## Goals

- Make the complete relative path structure of a directory-backed resource visible.
- Let users navigate directories and select individual bundled files without leaving the TUI.
- Preview the selected file in the existing rendered-content viewport.
- Preserve deterministic ordering, symlink safety, terminal safety, and bounded resource use.
- Reuse the current Bubble Tea model, viewport, keyboard, rendering, and request-generation conventions.
- Keep non-directory resources usable through the same full-view interaction.
- Provide a module whose filesystem-tree policy is testable independently of the top-level TUI model.

## Non-goals

- Editing, deleting, moving, or creating resource files.
- Executing scripts or other bundled files.
- Invoking an external file browser, pager, or syntax-highlighting executable.
- Adding a general-purpose filesystem browser to the CLI.
- Changing catalog discovery, resolver semantics, materialization, or resource selection.
- Following symlinks or displaying content outside the resolved resource root.
- Replacing the existing resource list tree with a shared generic filesystem/tree abstraction.
- Preserving the concatenated all-files document as the primary interaction. An aggregate view can be considered separately if a concrete use case appears.

## Background and context

The current resource list already presents a custom logical tree of scopes, kinds, registries, and resources. Selecting a resource opens a full-screen viewport. Directory-backed skills now render all regular files in deterministic path order, while file-backed resources render their single source file. That behavior makes bundled content discoverable, but it does not tell the user where files live or provide a way to focus on one file.

The full-screen detail view currently owns one viewport and receives rendered content through an asynchronous detail request. A filesystem tree introduces a second kind of selection: the selected resource remains stable while the selected path inside that resource changes. These identities must remain separate. Resource identity continues to use `detailKey`; bundle-file identity uses a normalized relative path such as `scripts/check.sh`.

The project already depends on Bubble Tea and Bubbles for the event loop, keyboard bindings, text input, and viewport. Bubbles includes a file picker, but that module models choosing a filesystem path and does not provide the preview-oriented tree behavior needed here. The application's existing primitives suffice to render the flattened tree, so this design adds no third-party tree dependency.

## Core concepts and mental model

The browser has three layers of state:

```text
resource row
    │ stable across catalog snapshots
    ▼
bundle root + bundle tree
    │ relative path identifies a file or directory
    ▼
selected file → asynchronous rendered preview
```

A bundle tree contains directories and regular files. Its visible representation is a flattened, depth-first list derived from the expanded/collapsed state. The flattening is a presentation detail; the underlying node paths remain the stable identities used for cursor restoration and content loading.

For a directory-backed skill, the root is the directory containing `SKILL.md`. If the resolved artifact path is already a directory, it is the root. If it is a file, its parent directory is the root. The initial tree expands the root and selects `SKILL.md` when it exists. For a non-skill file-backed resource, the tree contains one file entry and its preview behaves as it does today.

The browser should display relative paths using `/` regardless of host platform. Filesystem operations use `filepath`; artifact-facing relative paths use slash-separated `io/fs`-style names. This keeps the display and selection identity portable across macOS and Linux.

## User experience

### Layout

The existing full-screen detail pane becomes a split browser:

```text
+-- review ---------------------------------------------------------------+
| bundle                          | SKILL.md                             |
| ▼ review/                       |                                      |
|   • SKILL.md                    | # Review                            |
|   • notes.md                    |                                      |
|   ▼ scripts/                    | Review the change.                  |
|     • check.sh                  |                                      |
+--------------------------------+--------------------------------------+
| esc/q back · ↑/↓ navigate · ←/→ fold · Ctrl+C quit                    |
+-------------------------------------------------------------------------+
```

The left tree receives roughly 40% of the available width, with the preview using the remainder. The layout must still fit the documented 50-column minimum. At narrow supported widths, the tree may be reduced to a minimum usable width, but it must not be removed without an explicit product decision because discovering file paths is the purpose of this view.

### Navigation

- `j`/`k` and up/down arrows move through visible tree entries.
- Right arrow/`l` expands the selected directory.
- Left arrow/`h` collapses the selected directory; if it is already collapsed, focus moves to its parent when one is available.
- Enter on a directory toggles expansion. Enter on a file keeps that file selected; preview updates as soon as the file becomes selected.
- `g`/`G`, page keys, and viewport scrolling retain their existing meanings in the preview viewport.
- `Esc`/`q` returns to the resource list and cancels pending detail requests.
- `Ctrl+C` remains the hard-exit path.

Preview-on-selection makes the tree operate like a file explorer rather than a static index. Detail loads already run asynchronously and carry request generations, so rapidly moving across files cannot allow an older preview to overwrite the current selection.

The footer and help overlay must describe the bundle-browser-specific navigation while this pane is active. Existing list, modal, and overlay key meanings remain unchanged outside the browser.

## Key design decisions

### Standard-library filesystem traversal

The bundle-tree implementation should use `os.ReadDir` or `io/fs.ReadDir` to inspect one directory at a time. It should not parse output from `tree` or another command. Standard-library traversal provides typed directory entries, explicit error handling, deterministic sorting, and direct path-to-node mapping without relying on installed tooling.

Directory contents should be loaded lazily when a directory is first expanded. This avoids recursively enumerating or allocating a large bundle merely to open its detail view. The implementation may retain loaded children for the lifetime of the open resource so repeated collapse/expand operations do not repeatedly hit the filesystem.

### Separate bundle-tree module

The resource tree in `internal/tui/tree.go` groups domain rows. The bundle tree has different invariants: filesystem parentage, relative paths, directory expansion, regular-file filtering, and source loading. The implementation should therefore introduce a focused bundle-tree module rather than generalizing `flatItem` across unrelated domains.

The module should expose an interface around:

- loading or initializing a bundle root;
- expanding a directory and reporting filesystem errors;
- producing visible entries in deterministic order;
- moving the cursor and changing expansion state; and
- returning the selected relative path.

The top-level `Model` owns the module's lifecycle and pane routing, but should not duplicate path normalization, child ordering, or tree flattening logic.

### Selected-file preview instead of aggregate rendering

The current renderer's per-file behavior is retained, including Markdown rendering, JSON pretty-printing, source sanitization, hard line splitting, byte limits, and rendered-line limits. The browser passes the selected file path to the existing asynchronous detail-loading path.

This avoids rendering the entire bundle on every resize. It also applies the rendering limit to the file the user is examining rather than silently consuming the rendering budget across unrelated files.

An aggregate “show all files” action is deferred. It would be a separate presentation mode with its own navigation, truncation, and resize behavior, not a reason to complicate the initial browser design.

### Safety and deterministic behavior

The browser must preserve the existing detail-view safety contract:

- only regular files are previewable;
- symlinked files and directories are not followed;
- special files are ignored;
- relative paths are checked to remain within the resource root;
- names and errors are sanitized before terminal rendering;
- file reads are capped at the existing per-file byte limit;
- unbroken lines are split before Markdown rendering; and
- rendered output remains capped with a visible truncation marker.

Directory entries should be sorted deterministically at every level: directories first, then regular files, with each group sorted lexically by display name. The visible result is a stable depth-first tree, and the same input tree must therefore produce the same order on every supported platform.

The tree may omit symlinks entirely, matching the current detail contract.

## System interactions

### TUI model and requests

Entering the detail pane initializes the bundle tree, records the stable resource key, and selects the initial file. Selecting a file records its relative path and schedules a `requestDetail` command. The existing generation check remains the authority for accepting completion messages. A completion is accepted only when the detail pane, resource identity, request generation, and selected path still match.

A window resize re-renders the selected file at the new preview width. The tree itself does not need content re-rendering beyond layout width and cursor visibility. A snapshot refresh keeps the resource selected through `detailKey`; it should preserve the selected relative path when that path still exists and fall back to `SKILL.md` or the first visible file when it does not.

The left tree and right content viewport should be rendered as independent regions joined with Lipgloss. The existing terminal-fitting helper remains the final guard against exceeding physical dimensions.

### Non-skill resources

Commands, subagents, hooks, and MCP server definitions that resolve to one source file should use the same browser shell with a single file node. Their current JSON formatting and Markdown rendering behavior remains unchanged. A source-less or malformed resource continues to show the existing bounded placeholder or error state rather than attempting filesystem traversal.

### Logging and observability

Browser operation boundaries should use the existing structured `slog` logger: bundle initialization, directory expansion, selected-file preview requests, and filesystem/render errors. Logs must contain paths only where those paths are already permitted by the existing TUI logging contract; they must not include file contents or credentials. Routine cursor movement does not require a log entry.

## Error handling

Filesystem errors should be localized to the operation that failed:

- If the root cannot be inspected, the browser shows a sanitized bounded error in the tree or preview area and remains escapable.
- If a directory expansion fails, the directory remains collapsed and displays an error state without invalidating the rest of the tree.
- If a selected file disappears between enumeration and read, the preview shows a sanitized read error and the tree remains navigable.
- If a file is replaced by a symlink or non-regular file before reading, it is not previewed.
- If a stale asynchronous preview completes after selection changes, it is discarded without changing the tree, cursor, or current preview.

No filesystem error in the browser may trigger mutation, configuration changes, materialization, or process termination.

## Testing strategy

Tests should cover the module interface and the observable TUI behavior rather than restating implementation details.

### Bundle-tree behavior

- Root initialization selects `SKILL.md` when present.
- Nested directories and files produce the expected depth-first visible order.
- Expansion and collapse change only the expected visible entries.
- Directory and file ordering is deterministic.
- Relative paths use slash separators.
- Empty directories, missing roots, permission failures, special files, and symlinks are handled according to the safety contract.
- Cursor restoration uses relative path identity when the tree is rebuilt.

### Preview behavior

- Moving to a file schedules and displays that file's content.
- Markdown, JSON, scripts, and unknown text retain the existing rendering behavior.
- A stale preview cannot overwrite a newer selected-file preview.
- A selected file that disappears produces a bounded error without crashing the pane.
- Resize re-renders the selected file at the current width and preserves the scroll anchor where applicable.
- Source and rendered-output bounds remain enforced per file.

### Layout and interaction

- The split browser remains within the 50-column by 12-row supported minimum.
- Footer/help text exposes the browser navigation and quit path.
- `Esc`, `q`, and `Ctrl+C` retain their existing lifecycle semantics.
- Non-directory resources still open and preview correctly.
- Existing resource-list tree behavior and detail-pane request tests remain green.

The implementation should add focused unit tests beside the bundle-tree and detail model code. It should update the TUI product contract to document the browser layout, navigation, selected-file semantics, lazy traversal, and safety guarantees. Because the browser introduces a user-visible rendering path, a representative benchmark should measure bundle-tree initialization/expansion and selected-file preview rendering for realistic bundle sizes.

## Alternatives considered

### Shelling out to `tree` or similar tooling

Rejected. The executable may not be installed, its output is not a stable machine interface, platform output differs, and mapping a rendered line back to a selectable file would require fragile parsing. It would also undermine the project's static-binary and isolated-test properties.

### Reusing Bubbles `filepicker`

Not selected. `filepicker` is useful for selecting a path, but its interface does not model a paired tree-and-preview browser or provide the expansion and selection semantics required here. It could be reconsidered if the product later needs a file-selection modal unrelated to resource previews.

### Adding a third-party tree widget

Not selected. The project already has the required Bubble Tea primitives, and a dedicated bundle-tree module keeps the interface and safety policy local. A dependency would add API and styling constraints without eliminating the need to integrate asynchronous preview selection.

### Keeping the aggregate bundle view and adding a path index

Not selected as the primary design. It would expose names but leave users scrolling through a large concatenated document, make file-level preview less direct, and continue to spend rendering budget on files the user may not want to inspect. A future aggregate mode can coexist with the browser if demonstrated use cases justify it.

## Acceptance criteria

The design is implemented when:

- Opening a directory-backed skill presents an expandable relative-path tree and a selected-file preview.
- `SKILL.md` is the initial selection when available.
- Navigation and folding work with documented keyboard bindings and do not conflict with the outer resource-list workflow.
- Preview updates are asynchronous, stale-safe, bounded, sanitized, and deterministic.
- Symlink escapes and non-regular files cannot be selected for preview.
- File-backed resources retain their existing full-view behavior through the same browser shell.
- Supported-minimum terminal bounds and terminal restoration contracts remain satisfied.
- Unit tests, race-enabled tests, relevant benchmarks, formatting, and TUI QA checks are updated and passing according to repository guidance.

## Related documentation

- [TUI product contract](../tui-product-contract.md)
- [TUI and drift detection design](./2026-04-18-tui-and-drift-design.md)
- Implementation touchpoints: `internal/tui/app.go`, `internal/tui/state.go`, `internal/tui/actions.go`, `internal/tui/detail.go`, `internal/tui/keys.go`, and the new bundle-tree module/tests.
