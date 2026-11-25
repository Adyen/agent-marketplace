# Design

## Context

Diagnostics discovery health was carried by the query-scoped discovery projection, which is reset whenever the resource search changes. A directory source was accepted without checking that its path exists. Enter and Open resource already share one availability rule, and the local API can show content only for verified Marketplace skills.

## Goals / Non-Goals

**Goals:**
- Root-cause each reported defect with a failing test before fixing it.
- Keep fixes inside the GUI and its local API.

**Non-Goals:**
- Section order, Space key, post-install selection, and Update/Sync redesign.
- Visual redesign.
- CLI or TUI behavior changes.
- Inspecting directory and Git skill content, and native delivery of completion notifications; both need separate work.

## Decisions

- The local API refuses a directory source whose path does not exist before any configuration write, using a dedicated error code that the form shows inline.
- Diagnostics shows the discovery health of the unfiltered query, kept separately from the search-scoped result.
- Enter and Open resource remain gated by one rule; a regression test pins it for Marketplace, directory, and Git rows.

## Risks / Trade-offs

- Directory and Git skill rows still cannot be opened until the local API can serve their content.
