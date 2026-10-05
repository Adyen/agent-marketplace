## Why

The TUI startup snapshot already delivers progressively — provisional configured selections, a validated config stage that launches cached-first Marketplace discovery, a toggle-ready verified inventory, and a final assessed handoff — and the transient-acquisition path already publishes a browse-safe configuration while acquisition is blocked. This observable behavior is proven by existing tests but is not yet described by any spec, so the change that shipped it trips the spec-delta gate.

## What Changes

- Adds a new `tui-startup` capability describing only the current observable progressive-startup behavior of the TUI: staged snapshot delivery under one generation, verification-pending provisional rows from validated configuration, cached-first Marketplace browsing and browse-safe configuration while transient acquisition is blocked, toggle-ready verified inventory while drift is pending, the final assessed handoff, generation isolation of stale and superseded stages, mutation gating before verified inventory, fail-closed invalid configuration, and cached collection-revision expansion for provisional rows.
- Adds `// spec: tui-startup/<scenario>` annotation lines beside the existing tests that already prove each scenario; no test logic changes.
- **No behavior changes**: no runtime code changes; only spec prose and test-comment annotation lines.

## Capabilities

### New Capabilities

- `tui-startup`: the observable progressive startup behavior of the interactive TUI — staged snapshot delivery, provisional verification-pending rows from validated configuration, cached-first Marketplace browsing, browse-safe configuration during blocked transient acquisition, toggle-ready verified inventory, assessed handoff, generation isolation, mutation gating, and cached collection-revision expansion.

### Modified Capabilities

None. Selection precedence, acquisition-lock policy, and configuration layering remain governed by `config-layering`, `acquisition-lock-codec`, `marketplace-api`, and `owned-entry-reconciliation`; this capability specifies only the TUI's presentation and staging of those outcomes.

## Impact

- Documentation and specs only; no runtime behavior changes.
- Test files in `internal/tui`, `internal/marketplace`, and `internal/pipeline` change in comment lines only (added `// spec:` annotations on existing passing tests).
