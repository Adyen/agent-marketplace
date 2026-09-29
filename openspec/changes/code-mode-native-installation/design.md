## Context

The current `codeMode.enabled` switch injects a synthetic required MCP resource after resolution. The project already ingests a first-party `skills/` registry, expands collections, resolves ordinary MCP selections, and reconciles owned entries in shared harness files. The `mcp` command itself is the stdio server and is independent of its registration.

## Goals / Non-Goals

**Goals:** Reuse first-party resource ingestion, normal selection, and existing shared-file ownership without replacing the server runtime. Make old configuration explicit migration work rather than silently ignoring it.

**Non-Goals:** No hosted Marketplace publication or new auto-registration behavior. No replacement of the MCP server's search or execute implementation.

## Decisions

- Publish `skills/collections/code-mode/COLLECTION.yaml`, a skill under `skills/code-mode/`, and an MCP resource in the registry's recognized Claude source layout. Use an MCP definition whose command resolves through the consumer's installed CLI, not an absolute path from the registry publisher. Reject unavailable executable before claiming a usable installation. Alternatives: a hidden synthetic resolver entry or startup-side installation, both recreate the special opt-in path.
- Remove `codeMode` from config models and the injection path; recognize the obsolete key explicitly in every supported config root/fragment and return a coded actionable error before any mutation. Alternatives: silently ignore it, or auto-migrate, both risk misleading users or unexpected writes.
- Rely on ordinary collection selection and existing ownership state for remove, collision, and repeated sync. Avoid collection-specific pruning rules. A directly selected member stays selected when the collection is removed.
- Preserve the `agent-marketplace mcp` command as the server entry point. Its help no longer advertises the retired enable command; setup documentation shows the Git registry installation command.

## Risks / Trade-offs

- Executable names in registry-authored MCP entries use the consumer's PATH; installation needs a clear failure if the installed CLI is not reachable. An absolute publisher-side path would not work on another machine.
- Previously owned synthetic entries may coexist with or conflict with a registry-selected entry of the same name. Existing ownership reconciliation must preserve handwritten entries, avoid unsafe adoption, and report collisions instead of pruning without proof.
- Existing `codeMode` roots become unreadable until the user removes the field. Provide an exact migration diagnostic and preserve the original file bytes.

## Migration Plan

Users remove `codeMode` from their config, install `code-mode` from the first-party Git registry at `skills/`, and sync. Install a code-mode-capable binary before installing the collection. Rollback requires a build with the old opt-in behavior and the prior configuration; new installs use ordinary resource selection and cannot be converted back implicitly.
