## Context

The resolver selects winners among direct selections first, then applies layer exclusions and local selections, and then expands collections. Expansion skips a member whenever a resource with the same kind and installed name is already selected, in any install scope. The required-source rule is applied only while winners are selected among direct selections.

## Decisions

### Collection members are admitted per install scope

Expansion records selected installed names per install scope. A member is skipped only when its own install scope already has that installed name. Missing-member diagnostics are still suppressed when the name is selected in either scope, which keeps their current behavior.

### Required shadows are removed before the repository winner is chosen

For each installed name, the required machine-scope winner determines which repository occurrences shadow it: those that are non-required and come from another source. They are removed before the repository-scope winner is chosen, and one name-collision warning names the required winner and the removed sources. A repository occurrence from the required source can then win whatever the names of the other locations.

### Required resources revealed by expansion trigger one reselection

A required resource that exists only as a collection member is unknown when winners are first selected. After expansion, if a direct repository selection shadows a required machine-scope resource, selection runs once more with that resource known, so the repository winner is chosen exactly as for a direct required selection. The second pass can change only repository-scope choices. Repository layers cannot exclude machine-scope resources, and members are admitted per scope, so machine-scope output is the same in both passes. The second pass runs only when that shadow exists.

### A final pass removes collection-member shadows

Repository collection members are never direct selections, so no winner selection can remove them. After the last selection pass, any non-required repository resource that still shadows a required machine-scope resource is removed. Its source is added to the existing warning for that installed name, or a new warning is created.

### Collision warnings are logged once, for the final result

Name-collision warnings are logged after resolution completes, so a discarded first pass never logs a winner that is not installed.

## Compatibility boundaries

The change preserves existing same-scope direct-selection precedence, collection owner binding, nested-definition authority, exclusions, and additive hook behavior. It changes cross-scope admission and required suppression of repository selections only. Separate pre-existing gaps remain tracked in #424 (an optional machine-scope direct selection can hide a required collection member in the same scope) and #425 (unqualified member lookup can bind to the wrong layer when one registry is configured in both scopes). Neither is treated as fixed by this change.

## Verification

Resolver unit tests cover each combination of direct and collection selections across scopes. They compare machine-scope output against a resolution without repository configuration, and they check order independence across source names that sort before and after the required source. A command test syncs in both orders and checks `sync --check` from both locations. E2E cases cover the required collection member through the built binary. A benchmark measures the added passes.
