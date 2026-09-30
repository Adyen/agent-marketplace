## Context

See proposal.md. Server definitions reach the MCP client pool verbatim from the snapshot or the verified Marketplace artifact; only an empty declared `env` value is filled from the host. Harness compilers translate the same `${VAR}`, `${env:VAR}`, and `${file:PATH}` tokens into each harness target's syntax, so an installed server works while the same server fails in code mode. goja exposes Go structs by field name, which is why thrown `*Error` values and `Finding` values ignore their JSON tags. The same fix was already applied to MCP tool metadata. Registry labels come from `filepath.Base` of the source location because the v4 source name is not carried into the projected registry entry. Alias disambiguation counts entries in the `servers` argument, so a repeated ref counts as a collision with itself.

## Goals / Non-Goals

**Goals:** Make a declared server start with the same effective values an installed harness would use. Make every script-visible value match the advertised TypeScript declarations. Make refs predictable from configuration. Keep oversized results small enough to be useful to an agent.

**Non-Goals:** Supporting `${file:PATH}` or `${CLAUDE_PLUGIN_ROOT}` in code mode; changing how compilers translate interpolation; changing `promote` routing; forwarding host variables into child environments beyond the declared `env` keys; renaming Marketplace refs.

## Decisions

- **Interpolation grammar matches the compilers.** Recognize the same complete tokens as the harness compilers: bare `${VAR}`, `${env:VAR}`, and `${file:PATH}`. Expand the first two from the host environment in `command`, each `args` element, each `env` value, `url`, and each `headers` value, after artifact verification and before the pool digest or process start. Any `${file:...}` token, other well-formed `${...}` token, or reference to an unset variable fails `mcp.start-failed`. The message names the field and variable, never a value. A variable set to an empty string expands to empty. Text that is not a complete token is left alone, as in the compilers. Failing closed ensures a placeholder never reaches a server as a credential; file reads would widen the sandbox's host access, so they stay unsupported.
- **Expansion reads the host environment but does not add to the child environment.** The child still receives only declared `env` keys. Expanded values join the redaction set used for stderr and error bodies.
- **Startup is initialize plus the first request.** An HTTP or protocol rejection before the server has answered `initialize` is `mcp.start-failed`; failures after a successful handshake stay `mcp.tool-error`.
- **Deduplicate `servers` before aliasing.** Count distinct refs per name, so a repeated ref binds once under `<name>`.
- **Script errors are real `Error` objects.** Throw values created with the VM's `Error` constructor, with `name` `CodeModeError` and own properties `code` and `message` equal to the tool-level error. Go-cased properties are removed rather than aliased because the surface is unreleased; the one existing test that reads `err.Code` is updated.
- **All host values cross the VM boundary as plain JSON.** Reuse the JSON round-trip already used for tool metadata for catalog entries, findings, and `skills.load` results. The catalog is converted once per snapshot and copied per call so that script mutation stays per-call.
- **Refs use configured source names.** Carry the v4 source name into the projected registry entry. Directory sources label as `local:<source name>` and Git sources as `<source name>`. Entries without a name (legacy v2/v3 roots) keep today's derived label. Existing digest-suffix disambiguation still applies when two layers use the same label for different identities. The Marketplace `registry` field becomes `marketplace:<slug>` so that it equals the ref prefix. Alias disambiguation keeps using `<registry>-<name>` with the new value.
- **Truncation keeps a small preview.** The marker is `{"truncated": true, "bytes": <serialized size>, "prefix": <string>}`, where `prefix` is at most 4 KiB of the serialized value. It is cut on a UTF-8 boundary so the marker stays valid JSON. The 1 MiB cap still decides when truncation happens.

## Risks / Trade-offs

- Scripts or notes that hard-code today's `local:<directory>` refs stop resolving. They fail `resource.not-found`, which is visible and fixed by searching again. Session entries keep the ref they recorded, so `promote <recorded ref>` keeps working.
- Failing on unset variables turns a delayed 401 into an immediate `mcp.start-failed`. This is intended: the new error names the variable to set.
- A 4 KiB preview may hide the part of an oversized result the caller wanted. The `bytes` field and the marker tell the caller to narrow the query, which is cheaper than returning 1 MiB of context.

## Migration Plan

No configuration, lockfile, or cache format changes. Old session logs remain readable. Reverting restores the previous labels and shapes without leaving state behind.
