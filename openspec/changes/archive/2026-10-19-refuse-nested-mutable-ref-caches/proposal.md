## Why

The mutable ref cache stores each Git registry ref at `~/.cache/agent-marketplace/<registry-hash>/<ref>/`, so a slash-containing ref maps to nested directories. Mutable acquisition (discovery from a ref rather than a locked commit, for example TUI refresh and `update`) treated any existing directory at that path as a cached checkout and ran Git there. Git then discovered whichever repository enclosed the directory:

- With ref `foo` cached, ref `foo/skills/evil` resolved inside the `foo` checkout. Fetch and checkout moved the `foo` checkout's HEAD, and the nested ref's catalog was read from a subdirectory of the wrong tree. When the subdirectory did not exist, the nested ref was cloned inside the `foo` working tree, leaving its files there.
- With ref `foo/skills/evil` cached first, ref `foo` found a plain intermediate directory. Git discovery walked above the cache, so a repository enclosing the cache, such as a dotfiles checkout at HOME, received the registry's refs and was force-checked-out to the registry commit, replacing the user's tracked files. Acquisition reported success.

A repository checkout's committed configuration can select both refs of one registry, so this needs no access to the user's own state.

## What Changes

- Mutable acquisition refuses a ref whose cache directory lies inside another ref's cached checkout, before running Git or creating directories.
- Mutable acquisition refuses a ref whose existing cache directory is not its own Git checkout, before running Git.
- Refs whose cache directories only share intermediate directories, such as `feature/a` and `feature/b`, stay independently usable. The cache layout is unchanged.

## Capabilities

### New Capabilities

- `mutable-ref-cache`: each ref's mutable cache directory is its own checkout, and nesting refs are refused.

### Modified Capabilities

None.

## Impact

Mutable registry acquisition only. Exact snapshots, acquisition locks, `sync --locked`, and the cache layout are unchanged. Configurations that select both a ref and a ref nested below it from the same registry now fail for whichever of the two is acquired second, instead of silently corrupting a checkout. Caches written by earlier versions may already hold a nested clone inside another ref's working tree; the mutable ref cache is disposable, and removing the registry's cache directory clears it.
