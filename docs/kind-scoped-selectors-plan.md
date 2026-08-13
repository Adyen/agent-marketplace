# Follow-up Plan: Kind-Scoped Include/Exclude Refactor

## 0) Pre-step

- Merge the current PR first (scope-boundary enforcement).
- Start new branch: `kind-scoped-selectors`.

## 1) Schema redesign (breaking, no compatibility)

- Replace flat arrays with per-kind selector objects at all scopes:
  - Top-level config
  - Per-registry config
  - Local config
- New shape:
  - `skills.include`, `skills.exclude`
  - `commands.include`, `commands.exclude`
  - `subagents.include`, `subagents.exclude`
  - `hooks.include`, `hooks.exclude`
  - `mcp.include`, `mcp.exclude`
- Remove old global `exclude` semantics.

## 2) Config layer updates

- Update `internal/config` structs + YAML unmarshalling.
- Add strict validation for selector shape.
- Update editor helpers to write/read per-kind include/exclude paths.

## 3) Resolver refactor

- Move all selection/exclusion logic to `(kind,name)` matching.
- Keep precedence/required/default behavior unchanged, but per-kind.
- Keep machine-scope boundary warnings, now typed by kind.

## 4) CLI refactor

- Ensure `install/remove` always read/write per-kind selectors.
- Keep `--kind` as canonical UX.
- Update typed scope-boundary errors with hints for all kinds.

## 5) TUI refactor

- Toggle modal writes kind-specific exclude/include.
- Preserve machine-scope disable behavior for local exclude across all kinds.
- Update messaging to typed resource labels.

## 6) Test migration

- Update fixtures and expected YAML to new schema.
- Update resolver/CLI/TUI tests.
- Add explicit same-name cross-kind tests (e.g. `skill:x`, `command:x`) to confirm no ambiguity.

## 7) Commit strategy

1. `refactor(config): introduce kind-scoped selector schema`
2. `refactor(resolver): apply kind-aware include/exclude matching`
3. `refactor(cli): write/read kind-scoped selectors in install/remove`
4. `refactor(tui): modal toggles for kind-scoped selectors`
5. `test: migrate fixtures and add cross-kind ambiguity coverage`

## 8) Validation gate

- Run: `make ci-lint && make test`
- Fix all failures before push.
