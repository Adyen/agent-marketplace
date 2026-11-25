## 1. Repository-scope layout and inspection switch

- [x] 1.1 Switch `TargetFactory` project-scope hook aggregate in `pkg/harness-compilation/target/native_layout.go` to `settings.local.json#hooks`, keeping `hooks/hooks.json` as top-level marked legacy and retiring owned `hooks.json`.
- [x] 1.2 Update `nativeHookLayoutFor` in `internal/materialize/native_hook_inspect.go` so Factory Droid uses `settings.local.json#hooks` in both scopes.

## 2. Tests and documentation

- [x] 2.1 Add and annotate repository-scope tests (`// spec: hook-resources/...`) proving `.factory/settings.local.json#hooks` output without shadowing `.factory/settings.json`, atomic migration out of retired `.factory/hooks.json`, and coexistence with `.factory/settings.local.json` plugin registrations (`enabledPlugins`, `extraKnownMarketplaces`).
- [x] 2.2 Update existing repository-scope Factory test expectations across `pkg/harness-compilation`, `internal/materialize`, `cmd/agent-marketplace`, and `e2e`, and update docs (`site/docs/reference/harnesses-and-output-paths.md`, `skills/agent-marketplace/SKILL.md`, `docs/harness-validation/factory.md`).

## 3. Release gate and archive (after PR 1 ships in a stable release)

- [ ] 3.1 After PR 1 ships as a stable release, update `e2e/released_client_compat_test.go` and `.github/workflows/ci.yml` with the real release tag and verified Linux `amd64`/`arm64` SHA-256 checksums, and pass `released-client-gate`.
- [ ] 3.2 Run `openspec archive factory-hooks-settings-local-repo --yes` as the final commit before marking the PR ready for review.
