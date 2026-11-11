# Design

## Context

The local API exposes two read-only inspector routes. `/api/inspection` reads an uninstalled Marketplace skill's verified inspection projection over the network, and `/api/installed-skill-files` reads an installed Marketplace skill from its verified cache archive or staged directory. Rows advertise them with `inspectable` and `installedFiles`, and the GUI offers Enter and Open resource only when one of them is set. Directory and Git rows set neither. Their catalogs are ingested through a source filesystem rooted at the source root that refuses symlinks escaping that root, and the ingested skill already lists its supporting files.

## Goals / Non-Goals

**Goals:**
- Directory and Git skill rows offer the inspector through the existing installed skill files route and the `installedFiles` capability.
- Reads use the same source-root containment as ingestion.

**Non-Goals:**
- Editing skill files, a new inspector route, or any CLI or TUI change.

## Decisions

- **Reuse the installed skill files route.** The payload shape, opaque file IDs, preview limits, and GUI flow already fit; the route dispatches on the row's source kind. A separate route would duplicate the payload and the GUI request path for no user-visible gain. The `installedFiles` description in the contract is widened to name the new rows.
- **Source root, not skill directory, bounds reads.** Catalog entries record the absolute root their source filesystem was opened at. The bundle lists `SKILL.md` plus the ingested supporting files and reads each through a source filesystem opened at that root, so a symlink is followed exactly when ingestion would follow it.
- **Distinct error code.** A missing or refused local source returns `localapi.source_files_unavailable`, and the GUI shows copy that points at the source rather than the Marketplace cache. Marketplace failures keep `localapi.resource_files_unavailable` and their existing copy.
- **Change detection.** A file whose identity changes between listing and reading fails as unavailable, as installed staged files do.

## Risks / Trade-offs

- A Git row's exact snapshot may be pruned after the session snapshot; the read then fails with the actionable unavailable error rather than reading another revision.
