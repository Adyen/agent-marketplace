## Context

A root containing resources is ingested as one bundle. Independent nested bundles are not selected through that root. The warning needs enough context to let consumers restructure the source or narrow its entry path.

## Decisions

Use the discovery scan's existing sorted bundle evidence. Limit detailed output to five bundles and five resource examples per bundle, retaining omitted counts. Quote registry identifiers, paths, and resource names; never include raw load errors or temporary absolute paths in the detailed examples.

Retain the captured source root from primary ingestion for the ignored-bundle scan. Reopening the configured path could inspect a replacement directory and lose the original source authority.

Keep the warning diagnostic identity and existing error wrapping. The remote-registry context test uses a per-test HOME even though the package-level test harness already isolates the developer's home.

## Validation

Bind the mixed-root, registry-context, sorted/bounded, and captured-source scenarios to catalog tests. The captured-source regression must fail when the scan reopens the configured path, then pass when it uses the existing source. Run catalog, pipeline, CLI, and TUI consumer tests, the discovery benchmark, repository validation, and the strict specification gate.
