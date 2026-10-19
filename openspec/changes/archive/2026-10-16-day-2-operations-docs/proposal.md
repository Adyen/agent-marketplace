## Why

The public documentation says little about what happens after setup, when a registry renames, removes, or revokes a resource, when an operator changes organization configuration, or when a user edits a synced copy. Experiments against the CLI found five such behaviors that were wrong. Their fixes are in pull requests 705, 708, 710, and 714. These pages describe the fixed behavior.

## What Changes

- Add seven pages under `site/docs/`: an explanation of how upstream changes reach consumers; consumer how-to guides for renamed or removed resources, revoked resources, local changes to synced resources, and reviewing and rolling back updates; an author how-to guide for renaming or retiring a resource; and an operator how-to guide for rolling out organization configuration.
- Add the pages to the sidebar and link them from related existing pages.
- Correct the configuration reference: `git@host:` and hostless `file:///` origins are accepted.
- Describe the fixed behavior: `update` refuses atomically when a selection no longer resolves; a Marketplace 404 for one pin leaves other pins updating; plain `lock --scope organization` relocks changed sources; an edited file-mode copy is reported as a local edit to a managed copy.
- Pair every command with output from a binary built from `main` plus the four fix branches.

## Capabilities

### New Capabilities

None. This change adds and edits documentation only.

### Modified Capabilities

None. Runtime behavior and normative requirements remain unchanged.

## Impact

Markdown pages under `site/docs/` and the sidebar in `site/.vitepress/config.mts`. No runtime, test, or normative-spec changes. The pages describe behavior from pull requests 705, 708, 710, and 714, so they should merge after those. An update started from the TUI does not yet refuse atomically; the pages do not claim it does.
