## Why

The trusted include reader cleaned include paths lexically, so `..` after a symlinked directory read a different file than Git reads. It also treated `%(prefix)/` include paths as relative and silently skipped includes that Git applies from its installation prefix.

## What Changes

- Resolve relative and `~/` include paths by joining them without lexical cleaning, as Git does.
- Expand `%(prefix)/` include paths through the installed Git.
- Document the `%(prefix)/` form.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `git-authentication`: include path forms resolve to the same files Git reads.

## Impact

The shared authentication reader in `internal/cache`, its tests, and the private-registry documentation. No configuration or acquisition lock schema changes, new dependencies, or native harness changes.
