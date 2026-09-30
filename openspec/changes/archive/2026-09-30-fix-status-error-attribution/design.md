## Context

An error diagnostic identifies a resource name and harness target, but not its kind or install scope. Several selected resources may share that name.

## Decision

Carry precise identities for resources blocked before output, separately from the shared diagnostic format. Status matches those identities to selected resources. For later errors without a blocked identity, attribute a diagnostic only when its resource name and harness target identify exactly one status row. Ambiguous diagnostics remain in the errors section.

## Verification

Unit regressions cover cross-scope and cross-kind collisions, a materialization run verifies blocked identities, and existing diagnostic-severity coverage remains intact.
