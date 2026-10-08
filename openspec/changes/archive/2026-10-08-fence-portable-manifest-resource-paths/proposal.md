## Why

A portable manifest nomination with a matching ownership digest still authorizes pruning of any path below a harness target root. An independently authored nomination deletes shared harness configuration files and other root-level files such as `.claude/settings.json`, `.cursor/mcp.json`, `.devin/mcp_config.json`, `.windsurf/hooks.json` or `.codex/config.toml`. Shared aggregate files inside component directories, such as `.factory/hooks/hooks.json`, `.cursor/hooks/hooks.json` and `.claude/.claude-plugin/plugin.json`, are deleted the same way. A nomination of a component directory such as `skills` deletes every resource inside it, including handwritten ones. The Codex `.agents` partition is shared with Agent Marketplace configuration, so a Codex nomination with a matching partition root identity deletes `.agents/config.yaml` or `.agents/lock.yaml`. Each run reports a removal and no diagnostic.

## What Changes

- Refuse portable manifest nominations that do not name one resource inside a component directory of the harness target's native output, in repository and machine scope, for every harness target.
- Refuse nominations of, or containing, a shared aggregate file the harness target declares for the scope.
- Refuse Codex nominations outside the components their partition declares.
- Retain the `materialize.manifest.error` diagnostic and filesystem unsafe-path identity, preserving refused output and manifest evidence.
- Preserve genuine portable installation and pruning for every harness target in both scopes, including manifests written by earlier releases.
- Leave content-digest ownership of other resources inside component directories, SDK explicit output/prune authorization, and the accepted quarantine deletion boundary unchanged.

## Capabilities

### Modified Capabilities

- `materialization`: portable manifest nomination authority is confined to resources inside native component directories.

## Impact

One shared manifest-admission rule that replaces the Windsurf-only check, public consumer regressions with genuine native lifecycle controls for every harness target, and a declarative CLI E2E case. No public interface, manifest format, dependency, or authenticated ownership-state redesign. This does not give nominations inside component directories tamper-proof provenance.
