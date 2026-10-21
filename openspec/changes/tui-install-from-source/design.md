## Context

See proposal.md for motivation. The full flow, mockups, and error table are in
`docs/plans/2026-10-21-tui-install-from-source-design.md`; this document keeps
only the technical decisions.

`list --from` already previews an unconfigured source: it resolves web URLs,
checks the allowlist and registry path, resolves the default branch, and loads
catalogs over a synthetic configuration. `install --from` repeats the same
checks, writes the source into a staged layer, and runs dependency preparation,
which resolves the exact commit the acquisition lock records.

## Goals / Non-Goals

**Goals:** one preview implementation shared by `list --from` and the TUI; an
install that refuses a moved ref using the commit the lock would record; picks
that stay unambiguous across kinds.

**Non-Goals:** new CLI flags or output; changing configuration or lock formats;
changing the Space toggle modal or Marketplace discovery installs.

## Decisions

- **Shared preview.** Extract the body of `list --from` into a pipeline preview
  function returning the resolved URL, ref, path, commit, loaded catalog,
  bundle paths, and warnings. Alternative: a TUI-only copy. Rejected because
  the CLI and TUI would drift.
- **Preview commit.** Read it from the refreshed mutable ref cache with the
  existing remote-ref resolver. Directory sources have no commit.
- **Install commit check.** Dependency preparation already resolves the exact
  commit before acquiring the snapshot. It exposes that commit through the
  prepared install dependencies, and the explicit-source install compares it
  with the expected commit inside the staged transaction, so a refusal
  publishes nothing. Alternative: resolve the ref again before writing.
  Rejected because the ref could still move between that check and the
  commit the lock records.
- **Kind-qualified picks.** Add locators (kind and name, collections included)
  to install options for the explicit-source path; each locator is planned
  with its kind fixed, reusing the named-install planner. Name arguments stay
  for the CLI.
- **Bundle paths.** Catalog loading already appends one registry entry per
  sub-bundle with its relative path; the preview returns those paths when the
  source resolves to more than one catalog.
- **TUI integration.** A new modal pane routes q, Esc, and Ctrl+C before any
  form submits; the preview runs as an asynchronous request whose superseded
  responses are dropped; confirm reuses the guarded mutation path of
  Marketplace discovery installs.

## Risks / Trade-offs

- [A branch can move between preview and install] → the expected-commit check
  refuses and the modal returns to Pick with both commits.
- [Preview and install resolve the commit through different caches] → both
  read the remote ref; a mismatch only causes a safe refusal and re-preview.
- [Large sources] → the Pick list filters and scrolls; compact terminals drop
  descriptions.
