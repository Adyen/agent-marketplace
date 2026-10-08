## Decision

Accept only the residual hostile same-UID quarantine verification/deletion window. Keep the current checks and explicitly state that verification followed by name-based deletion is not atomic identity-conditional deletion.

The user approved documenting this boundary on 2026-10-08 rather than extending the local SDK into a sandbox against same-account programs. The accepted ADR explains the trade-off and the conditions for revisiting it.

## Preservation and scope

A replacement observed by final verification still requires the existing stale-prune conflict, unchanged foreign bytes and metadata, no unauthorized restoration, and owned-stage cleanup. Healthy pruning and truthful removal receipts remain required. No other filesystem, ownership, or concurrent-edit contract is excluded.

Clarify the existing scenario's timing without renaming it or adding an unproven scenario. Its existing annotated regression injects replacement before the final verification and remains the preservation and healthy-retry witness. Passing it cannot establish preservation after the final observation.

## Alternatives

Post-deletion link-count inspection may detect some failures but cannot recover the deleted replacement by itself. An owner-only directory does not exclude its owner. Additional checks retain a final gap. Platform-specific primitives require demonstrated Linux/macOS guarantees, not an atomic-rename analogy. OS-enforced denial of access to the quarantine namespace is a separate deployment design.

## Validation

Run the existing bound replacement-generation regression, strict pinned OpenSpec validation, scenario traceability, archive checks, and the repository validation gate. Review the full intended documentation diff for misleading preservation or atomicity claims and accidental broad exclusions. No attack payloads, live credentials, or new runtime behavior are involved.
