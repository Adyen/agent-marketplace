# Tasks

## 1. Local skill bundle

- [ ] 1.1 Record the captured source root on catalog entries loaded from a source filesystem.
- [ ] 1.2 Add a read-only local skill bundle that lists `SKILL.md` and the ingested supporting files and reads them through the source root with repository symlink containment, refusing missing files, escaping symlinks, special files, and files changed since listing.

## 2. Local API and GUI

- [ ] 2.1 Set `installedFiles` for directory and Git skill rows with a captured source root, and serve them from `/api/installed-skill-files`, returning `localapi.source_files_unavailable` when the source is missing or refused.
- [ ] 2.2 Update `api/gui-local.yaml`, regenerate `gui/src/generated/api.d.ts`, and show actionable source copy for the new error code.

## 3. Tests

- [ ] 3.1 Add Go tests for the bundle and the route, including an escaping symlink, a contained symlink, a special file, and a missing source, annotated with `// spec: gui-shell/unsafe-or-missing-local-skill-source-fails-actionably`.
- [ ] 3.2 Add a harness fixture and a Playwright spec that open a directory row and a Git row with Enter and with Open resource, check `SKILL.md` first, browse a supporting file, check that no files changed, and show the escaping-symlink error, annotated with `// spec: gui-shell/directory-and-git-skills-open-read-only-in-the-inspector` and the failure scenario.
- [ ] 3.3 List the new fixture in `skills/agent-marketplace-development/gui.md`.

## 4. Archive

- [ ] 4.1 `openspec archive gui-local-skill-inspection --yes`
