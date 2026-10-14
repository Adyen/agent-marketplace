## 1. Audit the operator guides

- [ ] 1.1 Record the reader and how-to quadrant for all ten operator guides, and decide whether any restructuring is justified.
- [ ] 1.2 Verify every command, flag, key, path, environment variable, endpoint, diagnostic and result against current source, deployment assets and governing specifications.
- [ ] 1.3 Rewrite task instructions and recovery steps, linking to reference or explanation rather than duplicating them.
- [ ] 1.4 Show exact or labelled typical output for commands, including silent success, and record practical isolated probes and static checks.

## 2. Verify the documentation

- [ ] 2.1 Review prose for active voice, needless wording, em dashes and internal-only values.
- [ ] 2.2 Run the frozen site dependency install and production build without dead links.
- [ ] 2.3 Run fresh TestDocsContract, `openspec validate --all --strict` and `make spec-check`.
- [ ] 2.4 Review the complete source-to-document claim audit without changing runtime, tests or normative specifications.
- [ ] 2.5 Run `openspec archive audit-operator-how-to-docs --yes` in the same pull request.
