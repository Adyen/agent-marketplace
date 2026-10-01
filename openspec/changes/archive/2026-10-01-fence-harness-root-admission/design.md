## Context

Native output spans seven harness targets, repository and machine scopes, and two Codex partitions per scope. Legacy roots, installation adapters and native aggregate routes all participate. Current config/lock boundaries reject symlinked .agents ancestors independently of harness directory aliases.

## Decisions

1. Resolve authorized scope anchors once: the repository or HOME may itself be a filesystem alias. Walk logical roots below the captured scope before canonicalization can hide a root or ancestor link.
2. Honor contained repository directory-root aliases and preserve existing explicit user machine-root relocation aliases. Root-link resolution must not lexically erase a normal component followed by a parent traversal, or use an external target as the new scope anchor. Components below an admitted root remain strict real directories.
3. Reuse shared filesystem descriptors for directory walks and creation, close every owned descriptor, and validate all root/plan/group ancestors for an output key before writing that key. Validate Codex's logical partitions before its canonical overlap preflight.
4. Keep typed unsafe-path refusal and existing per-target materialization diagnostic identities. A refused non-Codex target need not prevent independent safe targets; Codex preflight retains whole-run refusal before destination work.
5. Root admission and directory preparation do not claim descriptor-bound leaf publication, hostile same-user namespace exclusion or complete remediation of every filesystem consumer.

## Verification

Public materialize.Run regressions and built-CLI cases use inert resources, real native paths, both materialization modes, scope aliases and contained-root positive controls. Refusals assert diagnostics and unchanged external bytes, modes, mtimes, link identity and staging state. Existing valid root, Codex relocation and untouched-partition tests remain compatibility controls. Benchmark Run's disk paths before/after and run Linux/macOS, race, E2E/configuration and strict spec gates. Review all changed files, including tests and specs excluded by default filters.
