## Context

`list --from` validates the ad-hoc URL against the authored allowlist before any network access, then builds a synthetic one-entry configuration and loads its catalog. The catalog loader acquires Git entries through `cache.CloneOrPull`, which rejects an empty ref. `install --from` avoids this by resolving `cache.DefaultBranch` after allowlist validation, but `list --from` never did.

## Decisions

Resolve the default branch in `runListFrom` after the allowlist check and before building the synthetic configuration, only when the entry is not a local directory and no ref was supplied. Reuse `cache.DefaultBranch` rather than duplicating `git ls-remote --symref` handling, so `list` and `install` report the same failure and accept the same remotes. This keeps the allowlist as the first gate, so a blocked URL still fails before any network access.

*Alternative:* default the ref to `main`. Rejected because a registry whose default branch is not `main` would silently list the wrong branch (or fail), while `install` already follows the advertised `HEAD`.

## Validation

Bind the default-branch scenario to a CLI test that creates a bare remote whose `HEAD` advertises `trunk`, then asserts `list --from <url>` without `--ref` lists the resources. Keep the existing explicit-ref and local-directory tests as the regression controls. Run the CLI list tests, `make validate`, and `make spec-check`.
