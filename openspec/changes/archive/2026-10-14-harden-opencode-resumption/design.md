## Context

The OpenCode integration is opt-in. The pinned native executable is 1.18.34. Current shared engines remain the authority for safe source reads, publication, ownership, and exact acquisition.

## Decisions

- Fix the shared consumer boundary rather than introducing a second OpenCode filesystem engine.
- Keep adapters fixed and declarations data-only. Multi-resource bundles must publish one adapter alongside all accepted declarations.
- Use one pure 64-declaration budget validator for conversion and both sync modes/scopes before publication; retain the runtime safeguard and installed generations on refusal.
- Inventory native identities through the existing captured-file reader and shared aggregate size policy. Unknown identities in malformed or unsafe configuration block commands/agents, not independently safe skills.
- Refuse required OpenCode hooks with `hook.opencode.unsupported`; disableable native plugins cannot establish required enforcement.
- Ignore stdin EPIPE only when the handler has stopped reading; the handler's exit status and all output validation still determine success.
- Resolve tools through the authoritative mapping or tool policy, including explicit overrides. Unknown restrictive tools must not widen permissions.
- Retain explicitly authored native model IDs; never substitute an unrelated model for an unmappable canonical model.
- Native inspection must remain isolated, bounded, and free of uncontrolled dependency downloads and model calls.

## Validation

Start each verified fix with a failing regression and a valid control. Run affected packages, compiler matrices, native compatibility, the isolated hook probe, and repository gates. Record benchmark evidence for changed hot paths.

## Risks

The resumed branch predates current native hook and tool policies. Integration must retain current policies for existing targets without making unsupported OpenCode restrictions appear enforceable. Inactive native plugins remain inactive and cannot be advertised as required enforcement.
