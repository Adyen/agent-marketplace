## Context

`update-refuses-missing-selections` refuses before publication when a selection newly stops resolving. Its only remediation was a fixed hint that suggests `sync --prune-stale`. Reproduction against a file:// registry showed that the hint cannot be followed with the lock unchanged. `remove` of the old selection followed by `update` advances the lock, and `install <new-name>` then succeeds. `remove` handles collection members by writing an exclude, and it refuses `required` targets.

## Goals / Non-Goals

**Goals:** every refusal line names a command that works, or states the limit when it does not; prior-revision load failures are visible; the targeted registry escape is named.

**Non-Goals:** changes to `remove`, `install`, `sync --prune-stale`, or `status`; new flags; automatic pruning; Marketplace pin handling.

## Decisions

- **Per-miss remediation line.** Each named miss is followed by one line. The text for a non-required miss uses `skill.KindLabel(miss.Kind)` so that it round-trips with `skill.ParseKind`. The text for a required miss says that `remove` cannot drop it, so no remove command is suggested.
- **Load errors on the refusal.** `guardNewlyMissingSelections` collects non-nil prior snapshot load errors with their source instead of discarding them. The refusal prints them only when it refuses. The conservative rule is unchanged.
- **Fixed closing line.** It replaces the previous hint and names `update --registry <url>` as the escape for other registries.

## Risks / Trade-offs

- The remediation names `remove`, which deletes a configuration entry. The user must still choose to run it, and `update` does not run it.
