## 1. Audit the author guides

- [ ] 1.1 Record the reader and how-to quadrant for all six author guides.
- [ ] 1.2 Verify every command, flag, key, path, diagnostic and result against current source and governing specifications.
- [ ] 1.3 Rewrite task instructions and recovery steps, linking to reference or explanation rather than duplicating them.
- [ ] 1.4 Show exact or labelled typical output for commands, including silent success, and record practical isolated probes and limitations.

## 2. Verify the documentation

- [ ] 2.1 Review prose for active voice, needless wording, em dashes and internal-only values.
- [ ] 2.2 Run the frozen site dependency install and production build without dead links.
- [ ] 2.3 Run fresh TestDocsContract and scoped native validation.
- [ ] 2.4 Review the complete source-to-document claim audit without changing runtime, tests or normative specifications.
- [ ] 2.5 Run `openspec archive audit-author-how-to-docs --yes` in the same pull request.
