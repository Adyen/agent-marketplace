## 1. Registration shape

- [x] 1.1 Write the Factory catalog as a `.factory-plugin/marketplace.json`
  marketplace with an owner and relative plugin sources over linked
  generations (design D1); annotate
  `plugin-delivery/factory-catalog-is-a-marketplace-droid-accepts`
- [x] 1.2 Add the Factory registration member with project
  `settings.local.json` and user `settings.json` and its `local`
  marketplace source and `enabledPlugins` entries (design D2); annotate
  `plugin-delivery/factory-registration-preserves-handwritten-settings`
- [x] 1.3 Cover Factory collisions, removal, and the switch to decompose
  with tests that retire only owned entries

## 2. Probe and catalog

- [x] 2.1 Add the `factory-native-plugin` probe to the Factory
  harness-compatibility job (design D3) and register its identifier
- [ ] 2.2 When the probe passes in CI, declare Factory native delivery
  `verified` with the probe identifier; if it cannot pass, keep
  `unverified` and escalate the release decision

## 3. Released reader, documentation, and gates

- [x] 3.1 Add the native Factory released-client witness (design D4) as a
  required gate scenario
- [x] 3.2 Update the Factory harness-validation page and the plugin support
  reference to the probe and Factory's native status
- [x] 3.3 Run `make validate`, `make spec-check`, and `make validate-e2e`
- [ ] 3.4 `openspec archive plugin-native-factory --yes`
