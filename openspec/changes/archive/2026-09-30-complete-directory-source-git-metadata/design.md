## Context

The archived `directory-source-discovery` requirement already applies to every source layout, but the first implementation only filtered Claude. Factory, Cursor, Gemini, Windsurf, and Codex enumerate immediate skill subdirectories separately; Factory also follows directory symlinks, and Windsurf currently aborts on a missing `SKILL.md`.

## Goals / Non-Goals

Make the existing Git-metadata exclusion true for every supported source layout without changing how authored malformed skills, symlinks with other names, or model catalog fields behave. Do not add a generic hidden-directory policy.

## Decisions

Filter exact basename `.git` before each source layout treats a skills-tree entry as a skill candidate. Factory filters before its directory/symlink branches; other layouts apply the guard at their enumeration boundary. This avoids diagnostic suppression after ingestion and keeps Windsurf's hard failure for genuinely malformed authored skills. Devin already requires `SKILL.md` to admit a skill and needs no change. An all-purpose filesystem walker would introduce a new extension point and risk changing unrelated resource enumeration, so retain each layout's existing ingestion boundary.

## Risks / Trade-offs

A skill intentionally named `.git` can no longer be discovered; Git metadata has that reserved name. Five ingestion boundaries add small repeated guards, but a single global ignore policy would risk hiding authored content outside skill directories. Test each distinct enumeration contract and keep fixture roots confined to temporary directories. The stacked PR targets PR #276; if the parent head moves, review and verify the new candidate before publishing.
