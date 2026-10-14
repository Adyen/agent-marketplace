## Context

A canonical resource artifact is the resource's JSON encoding, including its `Ext` sidecars. The Marketplace payload decoder restores those sidecars, so a payload can carry a sidecar that disagrees with its canonical fields. Previews and resource assessments describe the canonical fields; the compilers consulted the sidecar first.

In-tree producers: the Codex ingester fills `Ext.Codex.MCP` from the same native values it uses for the canonical server. No ingester fills `Ext.Claude` or `Ext.Cursor`. Native hook projection already refuses a Claude or Cursor hook-type sidecar that contradicts the resolved type, and conversion away from Codex already refuses a Codex MCP sidecar that does not match the canonical server.

## Decisions

1. **One rule: a sidecar restates canonical authority and never widens it.** Each check compares the sidecar with the canonical value it replaces and suppresses the resource with an error on contradiction. Existing codes are reused so consumers keep one code per outcome: `subagent.security-restriction-unsupported`, the per-target hook-type codes native hook projection already emits, and `mcp.codex.security-unsupported` for Codex MCP output.
2. **Subagent tools allow restating or narrowing.** A sidecar entry is accepted when it equals a canonical entry or is a target tool that the canonical list covers when translated from its tool source through the shipped tool catalogs on the subagent surface (including invocation tool overrides). Translation is subset-only, so it never admits a tool the canonical list does not cover. An absent canonical list places no restriction, so any sidecar list only narrows it. An explicit empty canonical list admits only an empty sidecar. The shared check is exported so other compilers' tool sidecars can use the same rule.
3. **Hook types must be equal.** A hook-type sidecar that differs from the canonical type is refused in the ordinary Claude and Cursor translators, matching native hook projection.
4. **Codex MCP reuses the existing match.** The Codex target now applies the same canonical match that already guards conversion away from Codex. Codex-only fields with no canonical counterpart (bearer token and header environment names, remote environment variables) are unchanged.
5. **Modeled MCP connection fields are always reserved.** The shared MCP entry builder reserves `command`, `args`, `env`, `url`, `transport`, `headers` and the OAuth fields, with their case variants, regardless of typed presence. `disabled` stays reserved only when typed: an extension `disabled` value can only disable the server, so dropping it would be the widening.

## Non-goals

Gemini, Factory, Windsurf and Devin sidecars; Codex-only MCP fields without a canonical counterpart; tool vocabularies and mapping tables; foreign extension fields that are not modeled MCP fields.

## Verification

Regressions through the Marketplace payload decoder and public `Compile` fail before the change: canonical `Read` with a `Bash` sidecar compiles to `Bash` on Claude and Cursor; a prompt hook with a `command` hook-type sidecar compiles to an executable Claude hook; a Codex MCP sidecar replaces the command and forwards an extra variable; extension `command`/`args` become an active server on Claude, Cursor, Factory and Gemini. Positive controls show narrowing sidecars, equal hook-type sidecars and Codex-native MCP round trips unchanged.
