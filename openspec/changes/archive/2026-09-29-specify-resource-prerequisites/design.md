## Context

See proposal.md for motivation. The behavior lives in three places:

- catalog parsing reads `metadata.requires` from a resource's front matter extras;
- resolution compares each resolved artifact's prerequisites with the set of resolved kind/name pairs and returns unmet ones as warnings;
- each command renders those warnings: `install` filters them to the resources it selected, `sync` and `doctor` print grouped warnings, and `status` prints the `UNMET REQUIREMENTS` table.

Tests already cover the happy path: catalog parsing unit tests, resolver unit tests, `install` warning-filter unit tests, remediation formatting and redaction tests, and the three `mcp/*prerequisite*` E2E table cases. The existing `resource-prerequisites` scenarios annotate some of these.

## Goals / Non-Goals

**Goals:**

- Specify every externally observable rule of `metadata.requires` as it behaves today, quirks included, and prove each rule with an annotated test.
- Make the feature easy to find for registry authors and for consumers who see the warning.

**Non-Goals:**

- Changing behavior. Malformed items stay silently skipped. The user chose this explicitly. Surfacing them as diagnostics would be a separate change.
- Hosted Marketplace indexing, API, or web UI exposure of prerequisites. The Marketplace does not index them today.
- Auto-installing prerequisites and transitive or version-constrained requirements.
- Hook and MCP server prerequisites. Their native formats carry no documented front matter, so the spec names only skill, command, and subagent sources.

## Decisions

### Extend `resource-prerequisites` with ADDED requirements only

The existing requirements are correct and already bound to tests, so this change adds requirements next to them instead of rewriting them. A MODIFIED rewrite would move annotations without adding coverage.

- *Alternative considered:* a `baseline-resource-prerequisites` change. Rejected because baseline names are reserved for absorbing rows of `harvest-legacy-docs`, and no legacy document row maps to this capability.

### Specify quirks as they are

`kind: all` becomes a skill because kind parsing accepts `all` as "every kind", and every kind resolves to skill. Silent skipping writes only to the log file. The spec records both so that a later behavior change has to go through a spec delta. The docs describe `kind: all` as unsupported and advise against it, but do not promise that it errors.

### Documentation structure (Diátaxis)

| Page | Quadrant | Reader | Content |
|---|---|---|---|
| `how-to/author/declare-resource-prerequisites.md` (new) | How-to | Registry author | 1. Decide the prerequisite kind and name. 2. Add the structured item, with hints optional. 3. Use shorthand for the simple case. 4. Verify with `install` in a consumer project, showing the exact warning. 5. Verify with `status`, showing the `UNMET REQUIREMENTS` table. The page ends with troubleshooting for "no warning appears", covering malformed items that are skipped silently and inspection with `--debug`. |
| `reference/resource-prerequisites.md` (new) | Reference | Author or consumer looking up rules | Field table, the kind spellings table, the shorthand grammar, the malformed-item rules, the satisfaction rule, the warning and remediation formats, where each command reports the warning, and exit status, including `--strict`. |
| `how-to/author/declare-tags-and-prerequisites.md` → `declare-tags-and-licenses.md` | How-to | Registry author | Keeps tags and license. Its prerequisite step becomes a single link to the new how-to. |
| `reference/resource-metadata.md` | Reference | Author | Keeps the `metadata.requires` row and replaces the `requires item` section with a short summary linking to the new reference. |
| `how-to/troubleshoot-sync-drift.md` | How-to | Consumer | Adds a "`requires … but it is not selected` warning" entry pointing to the reference. |
| `glossary.md` | — | Everyone | Adds a **Prerequisite** entry and fixes the link in **Tag**. |
| `index.md`, `.vitepress/config.mts`, `reference/cli.md` (`UNMET REQUIREMENTS` row, exit-code row), `reference/registry-layout.md` | — | — | Link to the new pages and update the renamed path. |

The renamed page keeps no redirect. VitePress has no redirect support in this site, and the site is versioned with the repository, so every inbound link is updated in the same pull request.

- *Alternative considered:* expanding the existing pages. The user rejected it because it keeps the feature hidden inside a tags page.

### Test binding

| Scenario | Proof |
|---|---|
| Structured item without kind, Kind alias spellings, Kind all means skill, Shorthand without a kind, Surrounding whitespace is trimmed, Non-list requires value, Invalid item among valid items | New cases in the `internal/catalog` `requirementsFromExtra` table test, annotated once per scenario. |
| Command and subagent prerequisites | New `internal/catalog` loader test that ingests a Claude-layout command and subagent with `metadata.requires` front matter. |
| Malformed item stays silent | New E2E table case: a selected skill with only malformed items; sync passes, the skill exists, and output does not contain `requires`. |
| Prerequisite supplied by another registry, Same name with another kind | New `internal/resolver` tests. |
| Remediation with hints | Annotate the existing grouped-warnings remediation test in `cmd/agent-marketplace`. |
| Registry hint credentials are redacted | Annotate the existing resolver redaction test. |
| Unrelated unmet prerequisite is omitted, Collection member prerequisite is reported | Annotate the existing `internal/pipeline` warning-filter tests. |
| Rows are sorted | New `cmd/agent-marketplace` unit test for splitting requirement warnings with unsorted input. |
| Commands succeed with unmet prerequisites | Annotate the existing `mcp/install_skill_prerequisite` and `mcp/sync_reports_unmet_prerequisite` E2E cases, whose steps are all `MustPass`. |
| Strict sync does not promote unmet prerequisites | Add a `sync --strict` step to `mcp/sync_reports_unmet_prerequisite`. |

## Risks / Trade-offs

- [Pinning `kind: all` → skill as specified behavior makes a later fix a visible behavior change.] → That is intended. The docs discourage the spelling, so few authors depend on it.
- [The E2E assertion that malformed items print no warning could break if logging ever goes to stderr.] → Assert only that the output does not contain the prerequisite warning text, not that the output is empty.
- [Renaming the tags how-to breaks external bookmarks.] → The site has no redirects, and the page is new enough that the risk is accepted. The glossary and index links are updated.
