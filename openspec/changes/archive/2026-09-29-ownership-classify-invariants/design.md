## Context

`internal/ownership` already implements classification; consumer tests in
`internal/materialize` verify it end to end, and this PR's Rapid property
suite checks it against an independent model. This change records only the
verified contract.

## Goals / Non-Goals

**Goals:** one `owned-entry-reconciliation` capability with every scenario
bound to a `// spec:` annotation in a consumer test.

**Non-Goals:** no production changes; no specification of internal function
names or the test model as behavior. Claims are grounded in existing consumer
tests (`TestDevinUnmanagedMcpCollisionIsPreserved`,
`TestDevinMissingOwnershipStateNeverPrunes`,
`TestDevinDriftedOwnedEntryIsReplaced`,
`TestDevinStaleProvenEntryPrunesAndUnmanagedNeverDoes`,
`TestDevinRepairAdoptionRecordsOwnershipWithoutChangingBytes`,
`TestDevinUnmanagedValueConflictIsOfferedAsAThirdRepairClass`).

## Decisions

- Scenarios are phrased in observable terms (preserve, refuse, restore,
  prune, offer), matching the plan's fail-closed direction.
- Adoption and value-conflict replacement share one requirement because
  disjointness is part of the same repair report.

## Risks / Trade-offs

- Unverified nuances (diagnostic text, internals) are deliberately left
  unspecified.
