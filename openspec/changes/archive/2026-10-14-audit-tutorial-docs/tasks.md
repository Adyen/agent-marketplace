## 1. Audit the tutorials

- [x] 1.1 Record the reader and tutorial quadrant for all five tutorials, and decide whether any restructuring is justified.
- [x] 1.2 Verify every command, flag, key, path, environment variable, diagnostic and result against current source, deployment assets and governing specifications.
- [x] 1.3 Walk each tutorial against a locally built binary in a sandboxed HOME and record which steps ran and which were checked statically.
- [x] 1.4 Rewrite steps so each page follows one path with no choices and shows exact or labelled typical output, linking to other quadrants instead of duplicating them.

## 2. Verify the documentation

- [x] 2.1 Review prose for active voice, needless wording, em dashes, marketing language and internal-only values.
- [x] 2.2 Run the frozen site dependency install and production build without dead links.
- [x] 2.3 Run fresh TestDocsContract, `openspec validate --all --strict` and `make spec-check`.
- [x] 2.4 Run `openspec archive audit-tutorial-docs --yes` in the same pull request.
