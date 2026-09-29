## Why

Registry authors cannot tell from the documentation how `metadata.requires` behaves. The only coverage is a section of the tags how-to and a few paragraphs in the metadata reference. The `resource-prerequisites` spec covers the happy path but leaves out the parsing rules, how a prerequisite is satisfied, which commands report it, and what exit status results, so the code alone decides these today.

## What Changes

- Record the current behavior of `metadata.requires` in the `resource-prerequisites` spec. No runtime behavior changes. This covers:
  - which resource front matter is read;
  - structured and shorthand item forms, `kind` defaults and aliases, and whitespace trimming;
  - silent skipping of malformed items and of a non-list `requires` value;
  - satisfaction by kind and name from any source or configuration layer, with `registry` and `path` as hints only;
  - the warning message and remediation command, including credential redaction;
  - `install` reporting only for the resources it selected, collection members included;
  - `status` table ordering;
  - exit status under `install`, `sync`, `sync --check`, `sync --strict`, `status`, and `doctor`.
- Add or annotate the tests that prove each new scenario.
- Split `metadata.requires` into its own documentation pages:
  - a how-to for authors, *Declare resource prerequisites*;
  - a reference page, *Resource prerequisites*.
- Reduce the current tags how-to to tags and license, and link the new pages from the resource metadata reference, registry layout, glossary, docs index, sidebar, CLI reference, and sync troubleshooting guide.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-prerequisites`: adds requirements for declaration syntax and parsing, malformed-item handling, satisfaction rules, remediation text, install scoping, status ordering, and exit status. The existing requirements stay as they are.

## Impact

- Specs: `openspec/specs/resource-prerequisites/spec.md`.
- Tests: `internal/catalog`, `internal/resolver`, `internal/pipeline`, `cmd/agent-marketplace`, and `e2e/table_test.go` get new tests or `// spec:` annotations only. Production code is unchanged.
- Documentation:
  - new: `site/docs/how-to/author/declare-resource-prerequisites.md`, `site/docs/reference/resource-prerequisites.md`;
  - renamed and edited: `site/docs/how-to/author/declare-tags-and-prerequisites.md` becomes `declare-tags-and-licenses.md`;
  - updated: `site/.vitepress/config.mts` and the pages that link to the renamed one.
