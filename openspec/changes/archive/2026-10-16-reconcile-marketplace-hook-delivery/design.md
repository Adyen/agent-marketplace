## Context

The native hook pipeline now accepts immutable support records from verified
mirror archives. Reintroducing mutable staging directories would duplicate that
authority and lose the native publication, ownership, and cleanup guarantees.

## Decisions

- Keep the archive path as mirror provenance; pass cloned verified support bytes
  directly to native materialization. Validate declarations at both boundaries.
- Package flat adjacent assets below `hooks/`, preserving the plugin-relative
  command suffix. Do not rewrite commands in the Marketplace resolver.
- Skip the affected hook on unsafe support reads, with the established unavailable
  diagnostic. Do not publish a partial dependency inventory or block valid skills.
- Retain current native support installation modes and durable ownership proof;
  refuse byte-identical unowned mirror support rather than silently adopting it.
- Keep shell substitution quote contexts on a stack, so literal destination
  paths remain literal before, after, and inside nested command substitutions.

## Risks

The flat adjacent-source policy does not package nested source directories.
Those references retain a stable unsupported refusal and the Git-source remedy.
Existing script-less mirror revisions remain unusable until a new revision with
support declarations is selected.

## Validation

Run focused publication, inventory-integrity, native execution, refusal,
ownership, shell-execution, and drift tests; then the selected validation gate,
strict specification checks, and combined host E2E gate.
