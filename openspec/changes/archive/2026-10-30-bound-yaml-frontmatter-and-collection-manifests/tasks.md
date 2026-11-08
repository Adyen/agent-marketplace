## Implementation

- [x] Red/green regressions for skills, commands, subagents and rules: oversized frontmatter is refused, and frontmatter exactly at the ceiling loads.
- [x] Consumer witnesses: Claude ingestion keeps sibling skills; CLI skill discovery reports the oversized skill as malformed; collection discovery refuses oversized manifests and loads manifests at the ceiling.
- [x] The rule loader's lenient fallback cannot bypass the ceiling.
- [x] OpenCode ingestion reports the ceiling code rather than its generic invalid-resource code.
- [x] Retained before/after benchmark of the worst-case parse.
- [x] Validation, specs, E2E and adversarial review.
- [x] Archive as final commit.
