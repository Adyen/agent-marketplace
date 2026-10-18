## Why

FS08 in the R2 ledger (#431), finding F1 of the 2026-09-24 report. One target harness set applies to a whole run, and a repository's `harnesses:` list wins it. Machine-scope output (user and organization selections in HOME) is therefore reconciled against the repository's list.

A cloned repository that declares `harnesses: [claude]` makes a plain `sync` inside it remove the user's and organization's machine-wide output for every other harness. The output comes back when the user syncs somewhere else. `deinit --scope user` run inside such a repository removes organization output for the omitted harnesses. Reproduced on `main`: `sync` reports `Removed 2`, and organization and user Factory skills disappear from `~/.factory`.

Repository content must not decide what is installed outside the repository. The existing comment on target resolution already says project harnesses take precedence "for project operations".

## What Changes

- Target harnesses resolve per selecting scope. Output selected by repository or personal-local configuration keeps the repository list, then the user list, then the organization list.
- Output selected by user or organization configuration uses the user list, then the organization list. Repository and personal-local configuration cannot narrow it. Only when neither machine layer declares targets do those selections keep using the repository list, as before.
- Collection members follow the layer that selected their collection, and local selections follow the repository list, so neither cross-layer case changes.
- `sync` and `deinit --scope user` apply the rule, so teardown inside a repository preserves organization output for every machine target.
- `status` shows both lists, each with its source layer, when they differ.
- Per-source group `harnesses` still narrow either set.

## Capabilities

### Modified Capabilities

- `resource-selection`: target harnesses resolve per selecting scope.

## Impact

`pipeline.Resolve` computes a machine target set and its source, and passes the set to the resolver. The resolver chooses the set for each registry entry and Marketplace pin by the layer that owns it, and for each collection member by the layer that selected the collection. `deinit` checks the machine set before preserving organization output. `status` reports both sets when they differ.

Users whose repository list differs from their user or organization list keep their machine output for the machine targets inside that repository, instead of losing it there. Lock files do not record target harnesses and are unchanged.
