## Why

A repository portable manifest can nominate an unrelated repository file for Windsurf pruning when its digest matches. Repository-scoped Windsurf uses the repository as its planning base, so lexical containment below that base does not establish containment within the native `.windsurf/` output namespace. A public materialization regression deletes an independently authored `README.md` and reports success.

## What Changes

- Refuse repository-scoped Windsurf portable manifest nominations outside `.windsurf/`, including a nomination of the namespace root itself.
- Retain the existing `materialize.manifest.error` diagnostic and filesystem unsafe-path identity, preserving refused output and manifest evidence.
- Preserve actual Windsurf portable installation and pruning, machine scope, supported initial aliases, and other harness targets.
- Leave content-digest ownership within the native namespace, SDK explicit output/prune authorization, and the accepted quarantine deletion boundary unchanged.

## Capabilities

### Modified Capabilities

- `materialization`: repository Windsurf portable nomination authority is confined to its native output namespace.

## Impact

One shared manifest-admission check, a public consumer regression and declarative CLI E2E case with genuine native lifecycle controls, and a disk-I/O prune benchmark. No public interface, manifest format, dependency, processing budget, or authenticated ownership-state redesign. This closes only the demonstrated nomination scope defect, not all ownership-provenance or filesystem cases.
