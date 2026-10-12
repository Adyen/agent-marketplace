## Why

`list --from` requires an explicit `--ref` for a Git registry, so browsing an unconfigured registry fails with `validate registry ref: registry ref is required` unless the user already knows a branch name:

```text
$ agent-marketplace list --from https://github.com/anthropics/skills.git
Error: validate registry ref: registry ref is required
```

`install --from` already resolves the registry's default branch when `--ref` is omitted, and the published CLI reference documents the same default for `list`, so the two commands disagree and the documented browse-before-you-install flow is broken for Git URLs.

## What Changes

- `list --from` resolves the ref from the Git registry's advertised default branch (remote `HEAD`) when `--ref` is omitted, matching `install --from`.
- An explicit `--ref` or a ref parsed from a GitLab/GitHub web URL keeps precedence, and a local directory registry keeps ignoring the ref.
- A registry whose remote advertises no default branch fails with the same error `install --from` reports.

## Capabilities

### New Capabilities

- `ad-hoc-registry-browsing`: how `list --from` resolves the Git registry ref it inspects when `--ref` is omitted.

### Modified Capabilities

None.

## Impact

`cmd/agent-marketplace/list.go` and the `list --from` command path. `list` stays read-only: no configuration, acquisition lock, or materialized-resource writes change. Existing `list --from <url> --ref <ref>` and `list --from <local-dir>` behavior is unchanged.
