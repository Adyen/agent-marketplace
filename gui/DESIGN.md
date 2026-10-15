# Desktop GUI design

This document describes the visual and interaction design of the desktop GUI
(`gui/`). It combines two sources:

- **Visual language**: the agent-marketplace landing design
  (`agent-marketplace-design.html`). It uses an ink-alpha surface scale, one
  green signal color, monospace uppercase labels, `[*]` resource checks, and
  restrained motion.
- **Application shell**: the Codex desktop app. It has foldable left and right
  sidebars around a central work area, a compact toolbar with icon buttons and
  split buttons, and clean centered modals with a full-width primary action.

Behavior is defined in `openspec/specs/gui-*` and `session-*`. This document
covers presentation only. A restyle must not change what an action does, its
accessible name, or its keyboard path.

## Principles

1. **One surface per depth.** Panels are distinguished by a small change in
   tone, not by borders and shadows. The canvas, the sidebars, and raised
   elements each take one step on the ink-alpha scale.
2. **Tone before size.** Hierarchy comes from the primary, secondary, and
   tertiary ink tones. Font sizes change only at real title levels.
3. **Green is a signal, not decoration.** Green marks a state that is on,
   synced, or focused. It appears on checked resources, the synced status, the
   focus ring, and the selected tab. It is never used as a background fill
   for chrome.
4. **Mono for identifiers.** Resource names, kinds, scopes, registries, paths,
   counts, and keyboard hints use the mono face. Prose and controls use sans.
5. **Every action is a visible control.** Keyboard shortcuts speed up actions
   but never replace a visible, named button (`gui-shell` accessibility
   requirement).

## Tokens

### Ownership

`design/README.md` requires each application to own an adapter and forbids
importing another application's adapter. `gui/src/style.css` currently
imports `web/app/assets/css/design-tokens.css`. Replace that import with a
GUI-owned adapter, `gui/src/design-tokens.css`. The adapter imports
`design/semantic-colors.css` directly and defines the GUI tokens below. Use
the shared `--am-color-status-*` roles for warning, danger, and info.

### Color

The design's ink-alpha scale. `--ink-rgb` is `17 17 17` in light mode and
`255 255 255` in dark mode.

| Token | Value | Use |
|---|---|---|
| `--gui-canvas` | light `#ffffff`, dark `#111111` | Main work area |
| `--gui-ink` | `rgb(var(--ink-rgb) / 1)` | Primary text, icons |
| `--gui-ink-2` | `/ .607` | Secondary text, labels |
| `--gui-ink-3` | `/ .48` | Tertiary text, placeholders, hints |
| `--gui-line` | `/ .178` | Hairlines, input borders |
| `--gui-s0100` | `/ .042` | Sidebars, inspector, modal fields |
| `--gui-s0200` | `/ .074` | Buttons, kbd, tags, dividers |
| `--gui-s0400` | `/ .145` | Button hover |
| `--gui-layer` | `/ .042` | Hover or selection overlay on any surface |
| `--gui-signal` | `#00d16a` | On, synced, focus ring, selected tab |
| `--gui-signal-ink` | light `--gui-ink`, dark `--gui-signal` | Status text that reads as "good" |
| `--gui-solid-bg` / `--gui-solid-ink` | light `#111`/`#fff`, dark `#fff`/`#111` | Primary button |

Diff-style additions and removals in previews use the shared ready and danger
status roles at 12% alpha for the line background.

### Typography

The Adyen Sans and Adyen Mono fonts are not shipped in this repository;
bundling them is deferred. Until then, following Codex, which uses the
platform font, use these stacks:

- `--gui-sans`: `-apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", system-ui, sans-serif`
- `--gui-mono`: `ui-monospace, "SF Mono", "Geist Mono", Menlo, Consolas, monospace`

| Role | Size / weight | Notes |
|---|---|---|
| Base (`--x`) | 13px / 400, line height 1.38 | The desktop density used by Codex |
| Mono | 12px / 500, tabular numerals | Identifiers and counts |
| Caps label | Mono 11px / 500, uppercase, `--gui-ink-3` | Section eyebrows ("SOURCES") |
| Pane title | 13px / 500 | Toolbar titles |
| Modal title | 20px / 600 | One per modal |

### Space, radius, motion

- Spacing steps: 4, 8, 12, 16, 24, 32 px. Toolbars are 44 px tall. Sidebar
  rows are 28 px tall. Resource rows are 32 px tall, the same as the design's
  `.resource`.
- Radius: 4 px for tags, kbd, and rows. 8 px for buttons, inputs, and nav
  items. 12 px for cards and preview panels. 20 px for modals.
- Motion uses one easing, `cubic-bezier(.2,.8,.2,1)`. Colors fade in 150 ms.
  Sidebars slide in 200 ms. Modals fade and rise 4 px in 200 ms. When
  `prefers-reduced-motion` is set, all of these are disabled.
- Focus: a 2 px `--gui-signal` outline at a 2 px offset, shown only with
  `:focus-visible`.

### Theme

The theme follows the system setting by default. Settings → Appearance offers
System, Light, and Dark. An explicit choice is stored in `localStorage` under
`vitepress-theme-appearance`, the key the site and the Marketplace use. The
choice is applied before first paint by setting the `dark` class on
`<html>`. While the setting is System, the theme follows `prefers-color-scheme`
changes live.

## Shell layout

```
┌──────────────┬──────────────────────────────────────┬─────────────────────┐
│ ◧            │ Resources  user scope [⌕ ⌘K] [⟳] [Sync ~2] [Update] ◨ │
│              │                                      │ Details             │
│ ▤ Resources  │ ▾ PENDING SYNC                     2 │                     │
│ ◷ Pending  2 │  [*] skill  auth-preflight   reg  U  │ Kind     skill      │
│ ⌕ Search     │  [ ] mcp    docs-search      reg  U  │ Name     auth-pre…  │
│              │ ▾ INSTALLED                       14 │ Targets  Claude · … │
│ GROUPS       │  [*] skill  oncall-triage    reg  U  │ Status   enabled    │
│  Pending   2 │  …                                   │ [Remove] [Open]     │
│  Installed14 │                                      │                     │
│ SOURCES   ＋ │                                      │ SESSION             │
│  ▢ github…   │                                      │ DIAGNOSTICS  0      │
│  ▢ ~/code/…  │                                      │                     │
│              │ ~ 2 pending sync   ↑↓ move · space · s sync              │
│ ◉ Claude · 3 ⚙│                                      │                     │
└──────────────┴──────────────────────────────────────┴─────────────────────┘
  left sidebar           main column                       right inspector
```

The shell is a three-column CSS grid:
`[sidebar 260px] [main minmax(0,1fr)] [inspector 360px]`. A folded sidebar's
column collapses to `0`, and its contents are removed from the tab order with
`inert`.

### Left sidebar (foldable)

The sidebar uses the `--gui-s0100` surface and carries no border; the tone step
alone separates it from the main column.

- **Top bar** (44 px): a "Toggle sidebar" icon button (◧), placed like Codex's
  sidebar toggle. On macOS, the bar leaves 72 px of inset on the left for the
  window's traffic lights, and its empty area is a window drag region
  (`--wails-draggable: drag`).
- **Primary navigation**: icon rows that resemble Codex's "New thread",
  "Automations", and "Skills" rows.
  - *Resources* shows all groups.
  - *Pending sync* scrolls to that group and selects it. Its count is shown on
    the right in mono `--gui-ink-3`.
  - *Search* has a `⌘K` hint and opens the search field.
- **Groups**: a caps label, then one row per session group with its row count.
  Selecting a row scrolls the main list to that group's header and moves
  selection there, using the existing section selection.
- **Sources**: a caps label with a ＋ icon button ("Sources") that opens the
  Sources modal. Below it, one folder-icon row per configured source shows the
  location, truncated in the middle, with a "managed outside the GUI" or
  organization tag where it applies. Selecting a row opens the Sources modal
  focused on that entry. The list is read-only; the modal performs all edits.
- **Footer**: the configured Harness targets as a compact mono summary (for
  example `Claude · Codex +1`), a newer-version notice when there is one (a
  green dot and "v4.3 available"), and a ⚙ icon button ("Settings").

The active navigation row uses the `--gui-layer` overlay with 8 px radius, as
in Codex's selected-thread row.

### Main column

- **Toolbar** (44 px, a drag region except over controls):
  - Left: the title "Resources" (pane title), then "user scope" in
    `--gui-ink-3`, as in Codex's "Photobooth polish  photobooth".
  - Right, in order:
    1. A Search pill, styled after the design's `.search-btn`: an icon, the
       word "Search", and `⌘` `K` kbd hints. Activating it expands it in place
       into the search input. The local match status and a "Clear search" ×
       sit inside the pill.
    2. A Refresh icon button.
    3. A **Sync** button, styled like Codex's "Commit" button, with a sync
       icon and a pending badge (`~2`) in mono.
    4. An **Update** button, a separate visible secondary button next to Sync.
    5. A "Set up" button, shown only during first run.
    6. A divider, then a "Toggle inspector" icon button (◨).
- **Banners** sit under the toolbar for errors, the no-harness notice, and
  operation progress. A banner is a 32 px row on `--gui-s0100` with an icon
  and text. An operation in progress also shows a 2 px indeterminate progress
  line at the top of the main column, in `--gui-signal`.
- **Resource list**:
  - Group header: a sticky row with a caret, the caps title, and a count on
    the right. Selecting it toggles the group. This resembles Codex's
    "99 unmodified lines" separators.
  - Resource row: 32 px, mono, laid out as in the design's `.resource`:
    - check (`[*]` in `--gui-signal-ink` when on, `[ ]` in `--gui-ink-3`)
    - kind (`--gui-ink-2`)
    - name (`--gui-ink`, ellipsis; search matches use a `--gui-layer`
      highlight and weight 600)
    - registry (`--gui-ink-3`, ellipsis)
    - scope tag (right-aligned)
    - harness-override tag, when one applies
  - Hover and selection use the `--gui-layer` overlay; selection also has a
    2 px `--gui-signal` bar on the inline start edge.
- **Status bar** (32 px, bottom), styled after the design's `.activity` row
  and Codex's composer footer ("Local ▾ … main"). On the left, the sync status
  in mono, for example `~ 2 pending sync`, which changes to
  `✓ in sync` in `--gui-signal-ink`. On the right, keyboard hints:
  `↑↓ move · space select · ⏎ open · s sync`.
- **Notifications** are toasts stacked in the bottom center of the main
  column, styled as a floating pill on the `--gui-solid` colors, like Codex's
  "Revert all / Stage all" pill. The element remains the existing
  `aria-live="polite"` list named "Notifications". Error toasts carry a danger
  icon. Toasts stay visible until the next snapshot replaces them; there is no
  timer.

### Right inspector (foldable)

The inspector uses the `--gui-s0100` surface and has its own 44 px header with
the title "Details", resembling Codex's "Uncommitted changes" pane header. It
is a vertical stack of sections, each with a caps label:

1. **Resource**: the title `registry/kind/name` in mono, the description in
   `--gui-ink-2`, and a `dl` laid out like the design's `.term-dl`
   (`dt` in `--gui-ink-3`): Registry, Scope, Status, and Harnesses. Under it
   is an action row with the primary resource action (Install or Remove, as a
   solid button) and "Open resource" (secondary).
2. **Session**: drift and Harness targets.
3. **Marketplace discovery**: shown only when discovery is enabled. Each
   endpoint is a row with its label, cache state, and result count, plus the
   retry button.
4. **Diagnostics**: one row per diagnostic, with a severity icon, the message,
   and the severity color on the icon only. When there are none, the section
   shows "None".

The empty state, "Select a resource to inspect it.", is shown centered in
`--gui-ink-3`.

### Folding behavior

- The left sidebar folds with ⌘B and the "Toggle sidebar" button. The right
  inspector folds with ⌘⌥B and the "Toggle inspector" button. Both buttons
  remain visible when their panel is folded: the sidebar button moves into the
  main toolbar, as Codex does.
- The fold state of each panel is stored in `localStorage`
  (`am-gui-sidebar`, `am-gui-inspector`).
- When the window is narrower than 1000 px, the inspector starts folded and
  opens as an overlay sheet over the main column. Below 720 px, the sidebar
  behaves the same way. Escape and selecting outside the overlay close it.
- Folding a panel that contains the focused element moves focus to that
  panel's toggle button.

## Controls

| Control | Style |
|---|---|
| Icon button | 28×28, radius 8, transparent; hover `--gui-layer`; icon 16 px `--gui-ink-2` → `--gui-ink` |
| Secondary button | height 28, padding 10, radius 8, `--gui-s0200` background; hover `--gui-s0400` |
| Primary button | height 28 (toolbar) or 40 (modal), `--gui-solid-bg` / `--gui-solid-ink`; full width in modals |
| Tag | height 20, padding 6, radius 4, `--gui-s0200`, mono caps 11 px `--gui-ink-2` |
| kbd | mono 11 px, padding 2/4, radius 4, `--gui-layer` |
| Toggle switch | 36×20 pill; on: `--gui-solid-bg` track with a `--gui-solid-ink` knob, as in Codex's "Include unstaged" toggle |
| Checkbox | Nuxt UI `UCheckbox` themed so that its checked state uses `--gui-solid-bg` |
| Text input | height 32 (40 in modals), radius 8 (12 in modals), 1 px `--gui-line`, placeholder `--gui-ink-3` |

Nuxt UI components (`UButton`, `UModal`, `UCheckbox`) are
restyled through `app.config` `ui` theme overrides and the adapter tokens. Do
not override per instance.

**Icons**: Nuxt UI's default icon set loads from the Iconify network API,
which a desktop app must not depend on. Add a small `GuiIcon.vue` with inline
16 px SVG paths (1.5 stroke, round joins, as in the design file) for the icons
used: sidebar, inspector, search, refresh, sync, chevron, plus, folder, gear,
close, check, warning, info, error, file, and external. Set
`aria-hidden="true"` on every icon; the parent control provides the name.

## Modals

All modals follow the Codex "Commit your changes" pattern:

- Width 440 px; 720 px for First run; `min(72rem, 100vw - 2rem)` for Resource
  inspection.
- 20 px radius, `--gui-canvas` background, a large soft shadow, and a 24 px
  inner padding. The scrim is `rgb(0 0 0 / .32)` with a 2 px backdrop blur.
- Header: a 36 px icon tile (`--gui-s0100`, radius 10) at top left, a close ×
  icon button at top right, and the title (20 px / 600) below the tile.
- Body: label/value rows (label `--gui-ink`, 500; value right-aligned mono
  `--gui-ink-2`), group labels in 500 weight, inputs as specified above.
- **Choice lists**: Codex's "Next steps" list, a rounded group of 44 px rows on
  `--gui-s0100` separated by hairlines, each with a leading icon, a label, and
  a trailing ✓ on the selected row. These replace radio groups where a modal
  asks the user to choose one option.
- Footer: one full-width primary button. When there is a secondary or
  destructive choice, it sits below the primary button as a full-width
  secondary button.
- Focus is trapped while a modal is open. Escape is the cancel choice, and
  focus returns to the opener. The existing watchers already do this.

Per modal:

| Modal | Icon | Content |
|---|---|---|
| **Settings** (`HarnessSettings`) | gear | Section "Harness targets": a checkbox list, with organization targets shown read-only and tagged ORG. Section "Appearance": a System/Light/Dark segmented control. The "managed outside the GUI" panel sits at the bottom as a quiet `--gui-s0100` card. Primary: "Save". |
| **Sources** | folder | The source list as cards (location in mono, type tag, Remove icon button), then "Add source", which reveals `SourceForm` inline. Organization sources are read-only. |
| **First run** | sparkle | A three-step flow with a step indicator (`1 Targets · 2 Telemetry · 3 Sources`). Telemetry consent is a choice list with *no* preselected row ("Share basic telemetry" / "Don't share"), and Continue stays disabled until the user picks one. Cancel writes nothing. |
| **Resource action** (Install/Remove) | plus / minus | Label/value rows for the resource, the plan preview as a code card (12 px radius, mono, diff-colored lines), the risk acknowledgement as a toggle switch row, and the primary button "Install" or "Remove". |
| **Confirmations** (quit, sync-and-quit, migration) | exit / sync | The question, then a choice list with the affirmative, negative, and cancel options from `session-interactions`, then "Continue". Escape maps to cancel. |
| **Resource inspection** | file | Two columns: a file tree on `--gui-s0100` and a preview pane styled like Codex's diff panel, with a sticky path header (path in mono and a size tag), mono 12 px content, and line numbers in `--gui-ink-3`. |

## Keyboard map

The existing bindings stay: ↑/↓ move the selection, Space selects, Enter
activates, and Escape cancels the search.

New bindings, each of which mirrors a visible control:

| Keys | Action | Control |
|---|---|---|
| ⌘K or `/` | Open the search field | Search pill |
| ⌘B | Toggle the left sidebar | Toggle sidebar |
| ⌘⌥B | Toggle the inspector | Toggle inspector |
| ⌘, | Open Settings | ⚙ Settings |
| ⌘R | Refresh | Refresh |
| `s` (focus on the list) | Sync | Sync |

Single-letter keys are ignored while focus is in a text input or a modal is
open.

## Accessibility and test compatibility

- Keep every existing accessible name, role, and `id` on the controls that the
  Vitest and Playwright suites select (`Search`, `Search resources`,
  `Clear search`, `Refresh`, `Sync`, `Update`, `Set up`, `Sources`,
  `Settings`, `Quit`, `Open resource`, the group toggles, the `listbox` and
  `option` rows, and the regions "Resources", "Detail", "Notifications",
  "Marketplace discovery health", and "Diagnostics").
- **Quit** stays a visible control: an icon button at the bottom of the
  sidebar footer, also bound to ⌘Q through the native menu.
- The suites reference these classes: `workspace__row--selected`,
  `workspace__group-toggle--selected`, `workspace__name-match`, and the
  inspection, release, and settings classes listed by
  `rg '\.(workspace|resource-inspection|settings)__' src/*.test.ts tests/`.
  Keep these class names on the restyled elements so that tests continue to
  assert behavior rather than markup.
- New controls (panel toggles, the theme control, sidebar navigation and
  group rows) need accessible names, keyboard operation, and Vitest coverage
  of both.
- Contrast: `--gui-ink-3` on `--gui-s0100` must clear 4.5:1 at 12 px mono.
  Check both themes and raise to `--gui-ink-2` wherever a value falls short.

## Spec impact

The restyle changes no session behavior. Folding sidebars, the theme choice,
and the new shortcuts are additional presentation controls under the existing
`gui-shell` keyboard and accessible-name requirement. Commits that only
restyle carry `Spec-Impact: none — presentation-only GUI restyle`.

## Refinements from the first review

A review of the first shell build (1280×832, light theme, 83 enabled rows,
9 sources, 7 diagnostics) found the problems below. Each entry names the
observed defect and the rule that replaces the earlier text. Where a rule
here conflicts with an earlier section, this section wins.

### Window chrome

- *Observed*: the window keeps the native macOS title bar, so the title
  "Agent Marketplace" takes a 28 px band above the app, and the sidebar's
  72 px traffic-light inset pushes the "Toggle sidebar" button into empty
  space.
- *Rule*: on macOS the window uses a hidden-inset title bar with full-size
  content (Wails `mac.TitleBarHiddenInset()`). The traffic lights sit inside
  the sidebar's 44 px top bar, and the toggle follows them after the 72 px
  inset, as in Codex. The window title stays set for the Dock, the window
  menu, and accessibility. Linux keeps its native decorations.

### Resource rows: a name-first ledger

- *Observed*: each row prints `registry/kind/name` as one string, so the
  registry prefix varies from row to row, the alphabetical order of names is
  invisible, and the eye cannot find a name without reading the whole line.
  The kind column from the layout above was never built. The check glyph is
  `[x]`, not `[*]`.
- *Rule*: a row is a grid of fixed columns:
  `check 3ch · kind 9ch · name 1fr · registry minmax(0, 16ch) · scope 6ch · override auto`.
  - **check**: `[*]` when on, `[ ]` when off. The brackets are
    `--gui-ink-3`; only the `*` takes `--gui-signal`, in both themes. A
    column of black `[x]` read as noise; a column of green stars reads as
    "these are on".
  - **kind**: the kind ID in `--gui-ink-2` (`skill`, `command`, `subagent`,
    `hook`, `mcpserver`). Kind, registry, and scope use `--gui-ink-2`, not
    `--gui-ink-3`: at 11–12 px, ink-3 measures about 3.3:1 on white, below
    the 4.5:1 rule.
  - **name**: the display name in `--gui-ink`, weight 500, with search
    highlighting. It is the only element that carries weight.
  - **registry**: ellipsis, right-aligned before the scope.
  - **scope**: a quiet lowercase label for `user`, the default
    scope. `organization`, `project`, and `local` render as a tag
    (`--gui-s0200`), because they are the exceptions worth seeing. The
    element keeps its full scope text.
- Selection uses `--gui-s0200`, one step above hover (`--gui-layer`); before
  this change both used the same tone, and the selected row disappeared
  under the pointer. The 2 px signal bar stays.
- The sticky group header gets a hairline (`--gui-s0200`) on its bottom edge
  so that rows scrolling under it don't run into the caps title.

### Sidebar

- *Observed*: source rows print absolute paths that are cut at the end
  (`/Users/alexandergr/code/ai…`), so every row starts with the same prefix
  and the part that identifies the source is hidden. The folder icons
  shrink by different amounts as the label grows, so they don't line up.
- *Rule*: a source row shows a short label: the last two path segments for
  directories (`resources/skills`), the host for Marketplace sources, and
  the host and repository name for Git sources (`github.com/org/repo` →
  `org/repo`). The full location stays in the tooltip and the accessible
  name. The leading icon matches the source kind: folder for directories,
  branch for Git, globe for Marketplace. No icon may shrink: `GuiIcon` is
  `flex: none` everywhere.
- Group rows show a 6 px dot before the label in the group's tone:
  `--am-color-status-warning` for Malformed and Stale, danger for Broken, and
  `--gui-signal` for Pending when its count is above zero. Other groups have
  no dot. A dot appears only when the count is above zero.
- The footer collapses into one 32 px row: a signal dot and the harness
  summary in mono on the left, and the Settings and Quit icon buttons on the
  right. The summary has the visible prefix `targets` in `--gui-ink-3`, so
  `devin · factory` no longer floats unlabeled. The newer-version notice, when
  present, sits above that row.

### Toolbar and status bar

- *Observed*: Update is an outlined button beside the solid Sync button, so
  the toolbar has two heavy shapes competing for attention.
- *Rule*: Update uses the secondary button style from the Controls table
  (`--gui-s0200` fill, no border). Sync stays the only solid control in the
  window. Nuxt UI's soft neutral variant fills with `--ui-bg-elevated`, which
  is the canvas, so secondary buttons add the shared
  `.gui-button--secondary` class rather than remapping that token, which
  menus and popovers also read.
- The status bar's `✓` takes `--gui-signal` in both themes; the words
  `in sync` keep `--gui-signal-ink`. A pending count shows `~` in
  `--am-color-status-warning`.

### Inspector

- *Observed*: the resource title repeats the row's `registry/kind/name`
  string; the Session section is two loose sentences; and the Diagnostics
  section prints full raw messages, so seven warnings fill the whole
  inspector and push the rest out of view.
- *Rule — resource header*: a caps eyebrow `kind · registry`, then the
  display name as the title (mono 14 px / 500), then the description. The
  `dl` keeps Registry, Scope, Status, and Harnesses; Status leads with a 6 px
  dot (signal when enabled or installed, warning otherwise).
- *Rule — session*: each line is a key/value row with the key in
  `--gui-ink-2` and the value in mono, keeping the text "Drift: ready". Drift
  shows a status dot: signal for `ready`, warning for `pending`, danger for
  `failed`.
- *Rule — discovery*: each endpoint is a compact row: the label in 500
  weight, then `cache state · N results · truncated` in mono `--gui-ink-3`.
- *Rule — diagnostics*: the caps label carries the count. Each diagnostic is
  a disclosure row: the severity icon and the message clamped to two lines
  in mono 11.5 px, since the messages are tool output with paths. Selecting
  the row (click, Enter, or Space; it is a `button` with `aria-expanded`)
  shows the whole message. The full text stays in the DOM when clamped.
- The empty state shows the `file` icon above "Select a resource to inspect
  it."
- When the selection changes, the resource section fades in and rises 2 px
  over 150 ms. Nothing else animates on selection.

## Implementation plan

1. **Tokens and theme**: add `design-tokens.css`, the before-paint theme
   script in `index.html`, a `useAppearance` composable, and Nuxt UI theme
   overrides. Remove the import of the web adapter.
2. **Shell**: add `AppShell.vue`, a three-column grid with `Sidebar.vue`,
   `Inspector.vue`, fold state, shortcuts, and narrow-window overlays. Add
   `GuiIcon.vue`.
3. **Workspace**: split `WorkspaceView.vue` into `MainToolbar.vue`,
   `ResourceList.vue` (groups and rows), and `StatusBar.vue`. Keep the
   selection, search, and anchoring logic in the existing view and controller.
   Only the template moves.
4. **Modals**: add a shared `GuiModal.vue` (icon tile, title, close, body,
   footer slots) and `ChoiceList.vue`, then move all six modals onto them.
5. **Verification**:
   - `pnpm run check` (API drift, build, typecheck, Vitest) and
     `pnpm run test:e2e`.
   - Screenshots in light and dark at 1440×900 and 900×700, with each panel
     open and folded, and with each modal open.
   - A keyboard-only pass over every control.
