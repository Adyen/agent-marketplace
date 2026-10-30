## ADDED Requirements

### Requirement: YAML frontmatter stays within the parse ceiling

Resource ingestion MUST refuse a YAML frontmatter block larger than 64 KiB before parsing it, reporting the stable diagnostic `ingest.frontmatter.too-large` for that resource. The block is measured between its delimiters. The refusal applies to skills, commands, subagents and rules, and the rule loader's lenient fallback MUST NOT accept a block the ceiling refused. Other resources in the same source MUST still load. Frontmatter at or below the ceiling MUST parse as before. The ceiling has no override.

#### Scenario: Oversized YAML frontmatter is refused before parsing

- **WHEN** a skill, command, subagent or rule has a YAML frontmatter block one byte above 64 KiB, alongside a valid sibling skill
- **THEN** that resource MUST fail with `ingest.frontmatter.too-large`, the sibling MUST still load, and a block of exactly 64 KiB MUST still load
