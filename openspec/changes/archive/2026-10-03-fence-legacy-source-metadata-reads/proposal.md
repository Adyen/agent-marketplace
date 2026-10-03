## Why

Legacy skill frontmatter and best-effort authored-name recovery reopen resource paths after discovery/containment checks. Foreign aliases and hardlinks can contribute metadata independently of guarded native ingestion. These readers intentionally scan prefixes; switching them to whole-file snapshots would regress their existing work boundaries.

## What Changes

- Read prefix metadata through captured directory/destination authority with strict opened regular/single-link/nonblocking identity validation before and after the existing reader callback.
- Retain the legacy scanner's early termination and byte/line limits; bound streaming to captured file size without a new absolute threshold.
- Pass actual registry roots into legacy discovery and malformed-name correlation; preserve public best-effort helper, syntax behavior and legitimate contained file aliases.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: legacy source metadata prefix authority.

## Impact

R2 source-reader child of #482. Existing destination full-read publication APIs unchanged; no whole-source snapshot/new budgets or claims about preview/output/prune/aggregates/restore/ownership.
