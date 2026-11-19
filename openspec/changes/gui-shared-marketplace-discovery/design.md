## Context

The GUI local API already consumes `CachedDiscovery` and `DiscoverUpdates` and publishes accepted updates under a generation guard. The shared package on the current GUI target reads one page. PR #1066 merged the upstream pager and complete-catalog cache on `main`; its shared code also builds on the discovery changes from PR #1063.

## Goals / Non-Goals

Import the upstream shared discovery code and needed shared dependencies with their tests. Preserve GUI callers and generation handling. Do not merge unrelated upstream work, write a separate pager or cache, or implement source selection and independent GUI search and browse state.

## Decisions

- Use the shared implementation at `5dbe4c541ca8337f93d3814eacaed7913029624c` as the reference. Import only the shared discovery changes and necessary dependencies. Preserve any GUI-branch fixes. Report a conflict that cannot be reconciled within this scope instead of expanding it.
- Update existing GUI-path tests to prove the caller receives early and later pages, and use the upstream shared regression tests for cache, failure, cancellation, and endpoint policy.

## Risks / Trade-offs

Empty-query discovery changes for callers of the shared code. Verify existing desktop and TUI consumers, and do not widen allowed endpoints or treat failed partial catalogs as complete.
