## 1. Tests first

- [ ] 1.1 Add `internal/config/targets_alias_test.go` with `TestTargetsAliasRootAndGroupScopes`, `TestTargetsAliasRejectsBothSpellingsBeforeWrite`, `TestUserTargetsAbsentSnapshotAndExplicitEmpty` and `TestTargetsGroupCannotWiden`, annotated `// spec: consumer-config-format/<slug>`, `// spec: resource-selection/<slug>` and `// spec: config-mutation/<slug>` for every ADDED or new scenario they prove; verify they fail on the base commit
- [ ] 1.2 Capture pinned fixtures from the base-commit reader (absent user list, user `harnesses: []`, non-empty lists per layer) with their `sync`/`status` results, and add the test annotated `// spec: consumer-config-format/existing-harnesses-configurations-resolve-unchanged`
- [ ] 1.3 Add `tools/tui-qa/tests/targets-alias.spec.ts` covering the inherited-list snapshot, the `targets: []` save and reload, and the refused last-target removal with config and lock byte-identical; list it in `tools/tui-qa/README.md`; verify the refusal and save steps fail on the base commit

## 2. Implementation

- [ ] 2.1 Parse `targets:` at root and group scope, refuse both spellings, and restrict `targets: []` to the user root
- [ ] 2.2 Resolve an explicit empty user `targets:` list as a declared empty machine list in `sync`, `deinit --scope user` and `status`
- [ ] 2.3 Preserve the authored spelling in config editors, snapshot inherited lists, and refuse last-target removal without `targets:` spelling in the CLI editor and the TUI with migration guidance
- [ ] 2.4 Run the focused Go command, `make tui-qa-check`, `make tui-qa`, `make spec-check`, and `make validate`; verify all pass

## 3. Archive

- [ ] 3.1 Run `openspec archive targets-alias --yes`
