## Context

- Canonical resources use the Claude layout, but unmodeled frontmatter keys from every ingester land in the same `Extra` map, so compilation could not tell a Claude-authored `permissionMode` from a Cursor one.
- #248 solved the same problem for Gemini and Windsurf by reserving the target-native keys and emitting them only from a typed sidecar.

## Decisions

1. **Claude sidecar provenance.** `ext.claude.nativeControls` holds the Claude controls as authored. Claude ingestion copies them there, and keeps the extension copy so earlier clients decoding new payloads behave as before. Claude compilation removes every control key from the extension fields, then restores the sidecar values in the same emission slot, so Claude→Claude bytes are unchanged.
2. **Fail closed for missing provenance.** Payloads published earlier carry no sidecar, so their grant controls are dropped on Claude until republished (maintainer decision on #635).
3. **Restrictions win regardless of origin.** `SubagentReadOnly` reads typed sidecars (Claude, Codex, Factory) and extension fields alike, so an untyped `permissionMode: plan` or `readonly: true` is never dropped (maintainer decision on #636). Dropping an extension `permissionMode: plan` on Claude reports no omission, because the read-only rule re-emits it.
4. **Documented read-only modes only.** Mappings target documented modes: Claude plan mode, Cursor `readonly`, Codex `sandbox_mode`, the Factory `read-only` tools category. Gemini, Devin, Windsurf and OpenCode refuse, as does Factory when an explicit tool list would conflict with the category. OpenCode keeps its existing restriction code.
5. **Frozen digests.** The source-revision fallback projection clears `nativeControls` and maps the Factory category back to its former single-entry tool list, so existing lock identities are unchanged.

## Non-goals

- Mapping read-only to Devin or OpenCode tool or permission lists.
- Claude controls on targets other than Claude.
- Other Factory tools categories.

## Verification

Public `Convert` regressions fail before the change for foreign controls on Claude, the read-only mappings and the refusals. Positive controls cover Claude→Claude directly and through a Marketplace payload, plus an untyped `permissionMode: plan` compiled directly. A source-revision test pins the fallback digest basis.
