## Context

The repository config and personal-local config can live at different depths. See proposal.md for the observed failure mode and specs/workspace-resolution/spec.md for the externally visible rules.

## Goals / Non-Goals

**Goals:** Use one validated root pair for configuration reads, resolution, and output. Revalidate containment when a path may have changed since discovery.

**Non-Goals:** Change precedence, lockfile format, or permitted harness targets.

## Decisions

- Keep a repository root and an in-repository personal-local root together and derive the output root from the latter. Keeping three independently supplied roots would permit contradictory path combinations.
- Check lexical and physical containment, including missing path leaves below symlinks, before loaders or mutating operations. A lexical-only check would permit a symlink to redirect output outside the repository.
- Preserve repository-root config lookup when a command starts beneath a nested personal-local directory. Treating the nested directory as the repository root would silently ignore committed selection.

## Risks / Trade-offs

- Filesystem paths can change after initial discovery. Revalidation immediately before use rejects a retargeted symlink; downstream filesystem mutation still requires its own containment checks.
