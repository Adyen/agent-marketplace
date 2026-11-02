## 1. Probe

- [ ] 1.1 Reinstate the `claude-native-plugin` probe identifier in the
  harness validation suite and the plugin policy package
- [ ] 1.2 Add the Claude native plugin probe at user and repository scope
  with a pre-sync negative control, the no-Claude-state assertion, the trust
  precondition, and a live registration session (design D1, D2); annotate
  `plugin-delivery/claude-probe-loads-the-registered-plugin-at-both-scopes`
  and `plugin-delivery/claude-probe-fails-when-sync-wrote-claude-owned-plugin-state`

## 2. Catalog

- [ ] 2.1 Declare Claude native delivery `verified` with the probe (design
  D3); update tests that assumed Claude decomposes under `auto`; annotate
  `plugin-delivery/auto-delivers-claude-plugins-natively`

## 3. Documentation and gates

- [ ] 3.1 Document the probe and the trust precondition in
  `docs/harness-validation/claude.md` and regenerate
  `site/docs/reference/plugin-support.md`
- [ ] 3.2 Run `make validate`, `make spec-check`, and `make validate-e2e`
- [ ] 3.3 `openspec archive plugin-native-claude-verified --yes`
