# Tasks

## 1. Configuration archive

- [x] 1.1 Add tests annotated with `// spec: backup-recovery/<scenario-slug>` for every scenario of the change (contents, permission, missing files, no files, outside include and its debug log, command-start lock bytes, backup create output, per-day rotation, list group, restore refusal, disabled, `--no-backup`, read-only modes, failure ordering) and verify they fail before the implementation
- [x] 1.2 Capture the user configuration at command start and write the `agents-config` archive in the sync backup step and in `backup create`, refuse `backup restore --target agents-config`, and verify the focused tests pass

## 2. Documentation

- [x] 2.1 Describe the configuration archive, its contents, rotation, and manual `tar` restore in `site/docs/how-to/operator/back-up-and-restore.md` and `site/docs/reference/filesystem-layout.md`

## 3. Archive

- [x] 3.1 Run `openspec archive back-up-user-config --yes` and verify `make spec-check` passes
