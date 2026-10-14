## 1. Audit the top-level pages

- [x] 1.1 Record the reader and quadrant for `index.md`, `install.md`, `quick-start.md`, `glossary.md` and `terminal-basics.md`, and decide whether any restructuring is justified.
- [x] 1.2 Verify every command, flag, key, path, environment variable, diagnostic, version and result against current source and governing specifications.
- [x] 1.3 Run documented commands against a locally built binary in a sandboxed HOME and record which ran and which were checked statically.
- [x] 1.4 Rewrite each page so it stays in its quadrant, shows exact or labelled typical output, and links to other quadrants instead of duplicating them.
- [x] 1.5 Organize the index by reader, correct its user-journey description, and align the sidebar and nav order in `site/.vitepress/config.mts`.

## 2. Sweep terminology

- [x] 2.1 Align terms across `site/docs/` with the `openspec/config.yaml` glossary, limiting edits outside the five pages to the term or link.

## 3. Verify the documentation

- [x] 3.1 Review prose for active voice, needless wording, em dashes, marketing language and internal-only values.
- [x] 3.2 Run the frozen site dependency install and production build without dead links.
- [x] 3.3 Run fresh TestDocsContract, `openspec validate --all --strict` and `make spec-check`.
- [x] 3.4 Run `openspec archive audit-top-level-docs --yes` in the same pull request.
