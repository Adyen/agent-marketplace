## Context

Windsurf workspace ingestion loads a user root and then the workspace root; within each root it maps workflows, rules and skills by name, and the workspace root deliberately overrides the user root. The shared rule loader first parses frontmatter as strict YAML and falls back to a line-based parser for Cursor rules whose unquoted globs are not valid YAML. Codex ingestion already suppresses duplicate native identities without choosing a winner (`ingest.codex.duplicate-identity`).

## Decisions

1. **Refuse every claimant within one root.** A name claimed by two workflow files, or two rule files such as `deploy.md` and `deploy.mdc`, is removed entirely with one error per name, matching the Codex precedent. Choosing either file would let an added file replace a reviewed one.
2. **Keep cross-root precedence.** The workspace root overriding the user root is documented layering, not ambiguity. An ambiguous workspace name also removes the user root's resource with that name, so the refused override cannot fall back to a resource the workspace meant to replace.
3. **The tolerant parser rejects repeated keys.** Strict YAML already rejects them; the fallback now does too, so the loader reports the strict parser's error and the rule is not ingested.

## Non-goals

Skill directory collisions (directory names are unique per root), MCP server keys (JSON duplicate keys are already refused), and case-variant frontmatter keys.

## Verification

Public `Convert` regressions fail before the change: the later Windsurf workflow replaces the earlier one, and the repeated `alwaysApply` compiles to `alwaysApply: true`. An unrelated workflow is still emitted.
