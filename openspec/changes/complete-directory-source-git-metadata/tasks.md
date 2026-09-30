## 1. Prove cross-layout failure

- [x] 1.1 Add a test bound to `// spec: directory-source-discovery/non-claude-source-layouts-ignore-git-metadata` that covers Factory, Cursor, Gemini, Codex, and Windsurf directory-source skill trees; verify it fails on the PR #276 base because `.git` produces an error or false skill diagnostic.
- [x] 1.2 Add a test bound to `// spec: directory-source-discovery/windsurf-metadata-does-not-abort-discovery` with a real Windsurf layout and valid skill beside `.git`; verify its failure on the PR #276 base and success after the fix.
- [x] 1.3 Add a test bound to `// spec: directory-source-discovery/factory-git-metadata-directory-symlink-is-not-a-skill` with a Factory `.git` symlink to a directory; verify its failure on the PR #276 base and success after the fix.

## 2. Fix and verify

- [x] 2.1 Exclude only `.git` before Factory, Cursor, Gemini, Windsurf, and Codex skill-directory enumeration; verify focused tests pass and genuine malformed authored skills still retain their existing handling.
- [ ] 2.2 Run affected-package tests, the v4 CLI/TUI consumer tests, `make validate`, and `make spec-check`; verify explicit source roots and safety checks remain unchanged. Run applicable E2E and TUI QA gates if touched.
- [ ] 2.3 Run `openspec archive complete-directory-source-git-metadata --yes` as the final implementation commit in this pull request and verify the main spec retains every scenario.
