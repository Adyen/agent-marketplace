## Context

The install pipeline already chooses a scoped configuration destination and stages its edits with dependency reconciliation. Two repo-only guards prevent bulk selectors from reaching that path. The all-resources branch also hardcodes repository precedence when choosing a registry declared in several layers.

## Decisions

- Reuse the existing layer editor and synchronization flow instead of adding scope-specific installation paths.
- Remove the bulk scope guards and use the requested scope when choosing a registry declaration.
- Keep existing semantics: an unqualified tag remains a tag selector, a kind-qualified tag writes explicit locators, and `--all` writes the discovered snapshot rather than `all: true`.
- Retain directory-source write validation. Repository-relative directory sources cannot be promoted into user configuration.

## Validation

Extend the canonical-editor command matrix across all writable scopes, including absent target roots, exact layer locks, unchanged source configuration/locks, and native output. Add focused directory-source safety and personal-local subdirectory coverage, plus declarative end-to-end bulk scope journeys.
