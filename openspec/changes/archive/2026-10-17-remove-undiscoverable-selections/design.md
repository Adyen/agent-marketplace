## Decision

Supplement removal candidates with explicit configuration selections identified
by direct missing-resource or malformed-resource diagnostics. Recover their
source identity and layer from the loaded configuration, then reuse the existing
scope selection, ambiguity checks, required policy, layer editors, and sync.
Do not turn unresolved selections into materializable resources.

Map discovered sub-bundle diagnostics back to the configured parent root so
one authored selection is edited once. Resolve relative `--from` directory
arguments through the matched configuration entry before candidate lookup.

Use the same candidate discovery for configured and named `--from` removal.
Collection members are not direct selections and must not become synthetic
removal candidates. Discovery/acquisition failures remain errors, rather than
evidence that a resource disappeared.

Considering every configured include would expose valid collision losers and
change normal removal. Falling back only when no resolved resource matches
would silently choose a live resource over an ambiguous stale selection.
Diagnostic-backed candidates avoid both problems.

## Verification

Start with failing CLI regressions for disappearing and malformed selections.
Cover writable scopes, kind and source ambiguity, named `--from` kind inference,
includes, required resources, and unchanged unrelated configuration. Exercise
missing and malformed selections through declarative E2E cases.
