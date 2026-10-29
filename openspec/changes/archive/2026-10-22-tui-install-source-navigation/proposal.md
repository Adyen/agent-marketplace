## Why

Trying the install-from-source modal against a real repository showed that
moving between steps is slow and awkward: going back to Source and forward
again previews the source a second time even when nothing changed, the steps
cannot be walked with the arrow keys, and Review scopes can only be chosen
with letter keys. Every preview of a default-branch source also pays for a
remote lookup and a full fetch, even when the cache already holds the commit
the remote advertises.

## What Changes

- `←` and `→` move back and forward between the Pick and Review steps; `→`
  never installs. The Source step keeps arrows for text editing.
- Confirming the Source step with an unchanged URL, ref, and path reuses the
  previous preview and keeps the picks instead of previewing again.
- `↑`/`↓` (and `k`/`j`) move the Review scope selection between User,
  Project, and Local, refusing scopes the source cannot be written to.
- A source preview without a ref reuses the cache without fetching when the
  cached checkout is already at the commit the remote advertises for its
  default branch. A preview whose ref is a full commit SHA already checked out
  in the cache needs no network access.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-interactions`: adds arrow-key step navigation, preview reuse, and
  arrow-key scope selection to the install-from-source modal.
- `resource-installation`: adds cache reuse for source previews.

## Impact

- `internal/tui`: install-from-source key handling, footer hints.
- `internal/pipeline`: source preview chooses whether to fetch.
- `internal/cache`: default-branch lookup also returns the advertised commit;
  a check that a cached checkout is at a given commit.
- `tools/tui-qa`: install-from-source journey.
- Docs: `site/docs/reference/tui.md`, `docs/tui-product-contract.md`.

## Release note

Previews from `list --from` and the TUI install modal no longer fetch when the cached checkout already matches the remote's default branch, and a cached full commit SHA previews offline. In the install modal, arrow keys move between steps and scopes, and returning to an unchanged source keeps your picks.
