## Why

Most users who run `agent-marketplace install` want the resource for themselves on
this machine. Defaulting to the repository layer makes every personal install
write `.agents/config.yaml` and asks the user to commit it. Sharing through a
repository should be an explicit choice with `--scope repo`.

## What Changes

- `install` without `--scope` writes to the user configuration layer and
  materializes at the machine destination for named, collection, `--tag`,
  `--all`, `--from`, and Marketplace locator installs.
- Explicit `--scope repo` and `--scope local` keep their current behavior.
- The user-scope boundary is unchanged: `install --from <repository-relative-dir>`
  without `--scope` is refused with the existing repo-relative global config error
  instead of falling back to the repository layer.
- Marketplace endpoint registration keeps its existing user default.
- `remove` keeps its layer inference; only its `--scope` help text is corrected.
- The Landing scope panel identifies user as the default and shows
  `agent-marketplace install pdf --scope user`.

## Capabilities

### Modified Capabilities
- `marketplace-web-ui`: the Landing scope requirement is replaced by "Landing scope choices identify the user default" (the install default moved from repository to user scope).

### New Capabilities
- None. The install default is recorded as a resource-installation requirement.

## Impact

- Users who relied on `install` writing to the repository must add `--scope repo`
  (or `--scope repo` in scripts and documentation).
- Relative `--from` directories now require an explicit `--scope repo` to install
  into a repository.
- Affected code: `cmd/agent-marketplace/install.go` (flag default),
  `cmd/agent-marketplace/main.go` (remove help text), the Landing scope section,
  the public docs, and the command and web tests.
