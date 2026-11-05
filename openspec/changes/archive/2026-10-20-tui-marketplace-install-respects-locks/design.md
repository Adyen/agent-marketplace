## Context

CLI and TUI Marketplace installs share `PrepareMarketplaceInstall` and `CommitPreparedMarketplaceInstall`. The commit's post-install sync uses exact locked snapshots only when `InstallOptions.DependencyReconcile` is set. The CLI sets it to the command reconciler. The TUI builds discovery install options itself and left it unset, because `tui.DependencyLifecycle`, through which production wires lock-aware operations into the TUI, had no install hook.

## Goals / Non-Goals

**Goals:** make the TUI install use the same lock preparation as the CLI, and prove the discovery install flow end to end in a real terminal.

**Non-Goals:** changing CLI installs, consent policy, or discovery behavior; the misleading `e errors` footer hint (#824).

## Decisions

- **Wire the existing reconciler through `DependencyLifecycle`.** `internal/tui` cannot import the command package, and the TUI already receives its Sync, Update, and Mutate hooks this way. A new `InstallReconcile` field carries `reconcileCommandDependencies`, and the discovery install copies it into the options it prepares. The prepared plan keeps those options, so the commit's sync uses them too.
- **No extra verification scope.** The Mutate hook passes the same reconciler without a fresh exact-Git verification scope. A context without a scope verifies every snapshot open strictly, which is the safe default.
- **Journeys reuse the stub's installable skill.** The stub already serves a complete, verifiable archive for `org-review`. Letting a search find it, and optionally assessing it with one finding whose digest follows the Go canonical assessment identity, avoids generating a second signed artifact.

## Risks / Trade-offs

- [Lock preparation now runs on TUI installs] → it adds the same lock reconciliation work as a CLI install, and can surface the same lock diagnostics instead of silently installing unlocked content.
