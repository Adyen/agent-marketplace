# Windsurf validation

## Current status

Windsurf is a supported compiler and materialization target, but it is currently **config-only** in native compatibility CI. No verified credential-free noninteractive Windsurf CLI inspection surface is enabled.

The matrix records this as `unavailable`, with a capability-gap reason for each resource kind. Configuration assertions still run; the result is not native verification. Cascade's documented skills, workflows, rules, hooks, and MCP management surfaces are editor-hosted. An editor launcher or extension-package listing does not establish that those resource definitions were loaded.

## Configuration coverage

The shared compatibility scenario validates:

- repository and user output roots
- machine-specific workflow directory naming
- skills, commands, hooks, and MCP generated files
- file and symlink materialization modes
- collection expansion and catalog output
- preservation of unrelated aggregate content
- rules through conversion and expected diagnostics
- idempotence and `sync --check`

Native Cascade execution, authentication, model calls, hook firing, and MCP connections are not claimed.

## Enabling native validation later

Before adding a Windsurf native probe, document and pin a supported headless command. The probe must include a negative control, stable name/field assertions, bounded timeouts, credential isolation, and a local/no-network fixture strategy. A desktop or GUI container is not sufficient evidence for a CLI compatibility check.
