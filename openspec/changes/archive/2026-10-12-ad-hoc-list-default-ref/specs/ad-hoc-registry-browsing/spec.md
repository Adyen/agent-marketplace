## ADDED Requirements

### Requirement: Ad-hoc listing resolves an omitted ref from the registry default branch

`list --from` SHALL inspect the branch a Git registry's remote advertises as its default `HEAD` when `--ref` is omitted, matching `install --from`, so browsing an unconfigured registry does not require naming a branch. An explicit `--ref`, a ref parsed from a GitLab or GitHub web URL, and a local directory registry MUST retain their existing behavior, and an unresolvable default branch MUST fail the command rather than guessing a ref.

#### Scenario: Ad-hoc Git listing without a ref uses the default branch

- **WHEN** `list --from <git-url>` runs without `--ref` against a Git registry whose remote `HEAD` advertises a default branch other than `main`
- **THEN** the command MUST list that branch's resources with the existing `available` status
- **AND** it MUST NOT require an explicit `--ref` or write configuration, an acquisition lock, or materialized resources
