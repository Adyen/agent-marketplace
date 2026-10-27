## 1. Registration shape

- [x] 1.1 Write the Claude catalog as an accepted marketplace with an owner
  and relative plugin sources over linked generations (design D1); annotate
  `plugin-delivery/claude-catalog-is-a-marketplace-claude-code-accepts`
- [x] 1.2 Split the Claude registration member into project
  `settings.local.json` and user `settings.json` (design D2); annotate
  `plugin-delivery/claude-registration-preserves-handwritten-settings`
- [x] 1.3 Move owned registration entries recorded in the earlier file in one
  sync (design D3); annotate
  `plugin-delivery/claude-registration-moves-out-of-shared-project-settings`

## 2. Probe and catalog

- [x] 2.1 Add the `claude-native-plugin` probe to the Claude harness
  compatibility driver (design D4); annotate
  `plugin-policy-catalogs/claude-native-plugin-probe-loads-the-registered-plugin`
- [x] 2.2 Declare Claude native delivery verified for `claude-plugin` with the
  probe; annotate
  `plugin-policy-catalogs/shipped-claude-catalog-delivers-claude-plugin-packages-natively`

- [x] 2.3 Amendment 1: prime one Claude session before the enabled and
  inventory checks, record plugin list output, and declare
  `refresh: restart-required` (design D4)

## 3. Released reader and gates

- [x] 3.1 Add the released-client witness (design D5) as a required gate
  scenario
- [x] 3.2 Update `docs/harness-validation/claude.md` for the probe
- [x] 3.4 Fix the released-client witness failure from CI run one without
  weakening it
- [x] 3.3 Run `make validate`, `make spec-check`, and `make validate-e2e`
