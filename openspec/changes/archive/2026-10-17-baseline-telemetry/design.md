## Decisions

Baseline the runtime and command contracts, not the historical prose.
Resolution tests establish off-by-default behavior, exact organization level
policy, environment authority, endpoint metadata ownership, and community
report routing. Local OTLP fixtures establish actual disclosure and delivery.

Inventory tests establish detailed-primary-only routing, complete bounded
snapshots, sanitized locations, deterministic fingerprints, increasing
observation keys, and nonblocking best-effort delivery. Pipeline tests prove
that hard resolution errors suppress snapshots and that observations use the
effective resolution rather than ad-hoc browsing.

Diagnostic tests establish the detailed-level gate, review payload redaction,
acknowledgement, successful-export cleanup, failure retention, and separation
from the pending telemetry queue. The queue witness must inspect the actual
telemetry state root and prove that an export was attempted before asserting
no queue entry. Reset tests must compare old and new IDs and cover retained
diagnostic files, not just the command's success message.

Existing public pages retain configuration recipes and field tables. Delete
`docs/telemetry.md` and complete harvest task 1.12. This baseline does not
absorb the separate telemetry ADR or unrelated legacy documents.

## Discarded claims

- v2 organization consumer examples contradict exact-v4 root admission and
  are discarded; transport and organization policy are independent contracts.
- Calling basic attributes non-identifying is not a privacy guarantee:
  installation IDs are resettable pseudonymous identifiers, and detailed
  registry/resource metadata is intentionally disclosed.
- The diagnostic overlay is not a persistent fourth telemetry level.
- Build-target and release-history descriptions are historical packaging
  details, not a promise that every build contains a project endpoint.
  Routing remains conditional on a configured endpoint.
- Dated machine benchmark numbers, fixture-specific costs, and cardinality
  estimates are measurements rather than performance guarantees.
- Backend dashboard freshness-window recommendations are operator guidance,
  not behavior of this client. Snapshot values are observations, not leases.
- Internal exporter layout, worker names, package types, and span call-site
  lists are implementation details; wire instruments, disclosures, ordering,
  and best-effort semantics are preserved.
- Duplicate shell/YAML recipes and example addresses are explanatory, not
  additional behavioral scenarios.

## Validation

Run all telemetry, transport-config, logging, and affected pipeline tests,
the selected command suite, strict spec traceability, and the combined E2E
gate. No production telemetry code changes are required.
