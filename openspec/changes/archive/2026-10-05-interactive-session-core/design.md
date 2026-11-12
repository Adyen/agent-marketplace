## Context

See proposal.md for motivation. `internal/tui/state.go:265` currently keeps both domain/session state and terminal presentation state in `tui.Model`. The domain state includes authoritative and provisional discovery rows, discovery ordering, drift assessment, diagnostics, requests and generations, mutation gating, merged configuration, resolution state, and `DependencyLifecycle`; presentation state includes cursor, offsets, dimensions, folds, palette, and `zPending`. Startup lock/lifecycle setup and initialization Harness-target detection also remain wired from `cmd/agent-marketplace/tui.go`.

## Goals / Non-Goals

**Goals:** Extract the UI-agnostic session lifecycle and move user-witnessable interactive behavior into surface-neutral capabilities while keeping TUI behavior unchanged.

**Non-Goals:** Add GUI behavior in this layer, change CLI or TUI semantics, alter snapshot resolution or policy, or move terminal input/rendering requirements out of `tui-*`.

## Decisions

- Define an **interactive client** in each new capability's Purpose as the TUI or the desktop GUI acting on the user's behalf. Every `session-*` scenario is limited to behavior a user-scope GUI can witness; terminal mechanics and repository/personal-local scope assertions stay in `tui-*`.
- Extract staged snapshots, progress observations, and guarded actions as an event stream plus command API with no Bubble Tea types. Keep selection, viewport, terminal dimensions, folds, palette, and terminal input state in the TUI adapter.
- Move lock/lifecycle wiring (`prepareRuntimeLocks`, `refreshDriftedMarketplaceLocks`, and `reconcileCommandDependencies`) and `init` Harness-target detection and first-configuration logic from package main into the shared core. The TUI becomes an adapter to the core.
- Preserve scenario titles when moved whole; replace “The TUI SHALL” with “An interactive client SHALL”. For a split scenario, retain its TUI-specific assertions in a TUI residual and give the GUI-testable behavior a distinct neutral scenario. `Single-view provisional row continuity` uses “scope and harness indicators” in place of “scope letters”.

| Source requirement | Destination | Treatment |
|---|---|---|
| `tui-startup`: Progressive staged snapshot delivery | `session-startup` | Move whole. |
| `tui-startup`: Verification-pending provisional rows | `session-startup` and `tui-startup` | Split: neutral pending-row state and user-scope behavior; TUI residual retains toggle-modal behavior and committed/personal-local exclusions. |
| `tui-startup`: Browse-safe configuration during transient acquisition | `session-startup` | Move whole. |
| `tui-startup`: Verified inventory and assessed handoff | `session-startup` and `tui-startup` | Split: neutral verified inventory and assessment; TUI residual retains startup progress in the footer. |
| `tui-startup`: Selection and detail anchoring across stages | `session-startup` and `tui-startup` | Split: neutral resource/viewport anchoring; TUI residual retains screen-line and cursor-position guarantees. |
| `tui-startup`: Generation isolation and mutation gating | `session-startup` and `tui-startup` | Split: neutral generation and action gating; TUI residual retains sync/update key and Space/modal gating. |
| `tui-startup`: Cached collection revision expansion | `session-startup` | Move whole. |
| `tui-startup`: Single-view provisional row continuity | `session-startup` | Move whole; “scope letters” becomes “scope and harness indicators”. |
| `tui-startup`: Ordinary reloads retain the current view until final assessment | `session-startup` | Move whole. |
| `tui-navigation`: Grouped, ranked browsing; Accurate match highlighting; Complete installed Marketplace skill browsing; Read-only exact Marketplace skill inspection | `session-navigation` | Move whole. |
| `tui-navigation`: Single status-grouped resource list | `session-navigation` and `tui-navigation` | Split: neutral grouping, order, folding, filtering, and anchoring; TUI residual retains tab-bar and terminal-dimension behavior. |
| `tui-navigation`: Safe search input | `session-navigation` and `tui-navigation` | Split: immediate local filtering and debounced/cancellable remote search are neutral; paste routing, shortcut isolation, modal focus, and resize-required behavior remain TUI-specific. |
| `tui-navigation`: Visible applied filter | `session-navigation` and `tui-navigation` | Split: query/count visibility and query resumption are neutral; Esc clearing remains TUI-specific. |
| `tui-navigation`: Reachable keyboard help; Former tab shortcuts are inactive; Single-view resource browsing; Command palette typing remains distinct from navigation | `tui-navigation` | Unchanged. |
| `tui-feedback`: Observed progress and status; Stale progress isolation; Bounded notifications | `session-feedback` | Move whole. |
| `tui-feedback`: Context-aware command palette; Discoverable footer actions; Tab-free browse telemetry | `tui-feedback` | Unchanged. |
| `tui-interactions`: Multi-harness selection | `session-interactions` and a new `tui-interactions` requirement | Remove the mixed requirement and add a key-binding-only residual: g/p scope switching, Space toggle, and Enter submission. Neutral multi-selection, no-write toggles, validation, and unknown-ID preservation live in `session-interactions`; generic save/empty scenarios are not duplicated in the TUI residual. |
| `tui-interactions`: Explicit modal intent | `session-interactions` and `tui-interactions` | Split: guarded operation and post-write reload are neutral; q/Esc/Ctrl+C routing and accelerators remain TUI-specific. |
| Current TUI legacy migration behavior (`internal/tui/migration.go`) | `session-interactions` and `tui-interactions` | Add neutral user-personal migration behavior; TUI residual proves repository and personal-local roots are not offered or changed. |
| `tui-interactions`: Fixed-choice modal input ignores search filtering; Harness choices return after resizing | `tui-interactions` | Unchanged. |
| `tui-rendering`: Bounded and safe rendering | `session-rendering-safety` and `tui-rendering` | Split: inert hostile text is neutral; TUI residual retains terminal size and resize-required behavior. |
| Remaining `tui-rendering` requirements | `tui-rendering` | Unchanged. |
| `tui-mouse-navigation` | `tui-mouse-navigation` | Unchanged. |

The scenario assertions moved from `session-*` to TUI residuals are: `Provisional config stage publishes verification-pending rows`; `Excluded configured selections render unchecked in their verified position`; `Local all-true selections expand into pending rows on the normal config path`; `Registry-scoped local excludes apply to provisional rows`; `Verified inventory is navigable and toggle-ready while drift is pending`; `Verified rows inserted above a selection keep it on the same screen line`; `An untouched cursor stays on top when installed rows arrive`; `Mutations stay gated until verified inventory`; `Scope and selection without write`; `Accepting the offer migrates only the user-personal root`; and `Declining the offer continues through the legacy reader` (the last two retain repository/personal-local isolation only in TUI tests).

- Verify the migration baseline at `internal/tui/migration.go`: startup candidate discovery supplies only `paths.User`; accepting migrates paths carried by the prompt, while leaving unchanged returns to browsing without a migration write. The neutral scenario covers only the user-personal root; TUI residual scenarios assert the other roots remain outside the offer.
- Move annotations for requirements moved whole to `session-*`. Keep TUI annotations for residual scenarios and add TUI-surface annotations under `internal/tui/` or `tools/tui-qa/` for every new or split neutral scenario.

## Risks / Trade-offs

- [Staged startup or generation isolation regresses during extraction] → Move the existing TUI tests with the code, keep the TUI QA journeys unchanged, and verify with existing TUI tests plus `make tui-qa`.
- [The shared API accidentally absorbs view state] → Keep terminal selection, layout, folds, palette, and key routing owned by the TUI adapter and test the core without Bubble Tea types.
- [A neutral scenario cannot be exercised by a user-scope GUI] → Audit every session scenario against the scope and interaction boundary before archiving; keep terminal and repository/personal-local cases in TUI residuals.
