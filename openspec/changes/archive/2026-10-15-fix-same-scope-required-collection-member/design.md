## Context

Collection expansion records the installed names each install scope already selects and skips a non-additive member whose name is already selected in its own scope. The check ignores required policy, so a non-required direct selection hides a required member. The cross-scope machinery (required shadows, reselection after expansion) handles only non-required repository artifacts shadowing required machine artifacts; it never sees two machine-scope selections, so it cannot repair this case.

## Decisions

### A required member displaces a non-required selection at admission

Expansion tracks, per installed name and install scope, whether a required selection holds it. The flag is OR-ed over the direct selections, because winner selection can keep a required and a non-required direct winner for one scoped name when they come from the same source URL at different paths; a required member must then still be skipped and must not displace the other. A member is skipped when it is not required or the holder is required. Otherwise it is admitted and recorded as displacing.

### Removal and warning happen in one pass after expansion

Displaced selections are removed from the expanded artifacts in a single pass, so artifact order is preserved and a direct selection configured after the collection is removed as well. The warning names the member as winner and the displaced sources as runners-up, and merges into an existing warning with the same winner so a resource is reported once even when winner selection already reported a removed repository shadow. Displaced selections from the member's own source location add no runner-up, mirroring same-content deduplication; with no runner-up there is no warning.

### Unchanged behavior

Hooks keep their additive handling. Direct beats member and first expansion wins among equally required selections. The cross-scope machinery is unchanged.

## Alternatives considered

Extending the required-shadow detection to same-scope artifacts was rejected. It would need a rule that drops same-source non-required selections, contradicting the rule that a repository copy from the required source is kept, and it runs after expansion only for repository artifacts.

## Verification

Resolver tests cover both source kinds and both configuration orders, the combined machine and repository case, a same-source control, and a required-direct control. A command test syncs in both orders and checks `sync --check` from the repository and from the home. An E2E case covers the built binary.
