## Context

Materialization prunes the component directories a target's native layout declares. The Gemini compiler has written `.gemini/agents/<name>.md` since the import commit 6414a3e6, but the layout never declared an `agents` component, an omission rather than a decision. In symlink mode, the default, the directory was therefore planned only while a Gemini subagent was still selected: deselecting the last one left its link in place and `sync --check` found nothing to repair. File mode already removed it.

## Goals / Non-Goals

**Goals:** `sync` and `deinit` remove deselected Gemini subagents, handwritten subagents survive, and machine-scope backups cover the directory.

**Non-Goals:** how Gemini subagents install, their link targets, blocked-registry removal, the Gemini command path and other harnesses.

## Decisions

- **Declare the component only.** The component alone makes `sync`, `deinit` and the backup collector cover `agents/`. Link targets of existing installs do not move, so an upgrade needs no migration: the first `sync` reports no drift for a selected subagent and removes the leftover link of a deselected one. Alternative: also declare the subagent resource path, which completes the layout but moves the link target of every existing install and changes blocked-registry removal. Rejected for this fix.

## Behavior change

- `agents` joins the declared component directories that every `sync`, `sync --check` and `deinit` validate even when nothing of that kind is selected, so a `.gemini/agents` symlink to an existing directory is now refused with `materialize.destination.error`, as `.gemini/skills` and `.gemini/commands` already are under the existing materialization rule that output-directory ancestors below a harness root are real directories. A dangling link is still ignored.

## Risks / Trade-offs

- [The layout stays short of the compiler output for the subagent path] → tracked in the follow-ups below.

## Follow-ups

- The Cursor, Factory and Windsurf layouts declare a `rules` resource path but no `rules` component, the same gap.
- Declare `agents/%s.md` in the Gemini `ResourcePaths`: Gemini subagents would install as resource links like Cursor, Factory and Devin subagents, a skipped one would hold back only `agents` in `blockedComponentsFor` instead of the whole Gemini output, and it would join the resolver's case-fold name grouping.
- The Gemini layout declares `commands/%s.md` while the compiler emits `commands/%s.toml`.
- Machine-scope `sync` takes no safety backup before it prunes the last Gemini resource of the scope, because `CollectManagedPaths` covers only harnesses that still have a selection. This predates the change.
