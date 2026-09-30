## Why

A CLI edit into the user layer failed when `~/.agents` did not exist yet and the home directory sat under the macOS `/tmp`, `/var`, or `/etc` alias into `/private`, as temporary and sandbox homes do. `install <name> --scope user` then reported a missing file or a lock reconciliation conflict instead of creating the user root. Repository and personal-local edits already create their missing roots. A config path through a dangling symlink, such as a `~/.agents` dotfiles link whose target is not checked out, also had no defined behavior.

## What Changes

- A CLI edit into the user, repository, or personal-local layer creates a missing root as v4 and applies the edit, whether or not the root's directory exists, including under the macOS `/private` aliases.
- A new user root declares the default harness targets `factory` and `windsurf`, the list `init` writes.
- A Marketplace install into user scope creates the user root and its acquisition lock under the same conditions.
- A CLI edit refuses a layer whose root config file, or a directory above it, is a symlink to a missing target, and writes nothing through it. Read-only commands and `sync` keep treating that layer as absent.

## Capabilities

### New Capabilities

- `config-mutation`: How CLI edits create and write configuration layer roots.

### Modified Capabilities

None.

## Impact

User-scope `install` for Git, directory, and Marketplace resources, and every CLI edit that can create a layer root. No configuration, acquisition lock, or materialized output format changes.
