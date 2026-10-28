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

## 2. Native status

- [x] 2.1 Keep Factory native delivery `unverified` after CI runs
  40682001059 and 40698987206 showed that Droid recorded the owned marketplace
  but did not install the enabled plugin in headless metadata sessions
  (design D3); ship no Factory probe or probe identifier
- [x] 2.2 Keep `auto` decomposing Factory plugins and explicit
  `delivery: native` refused; leave the Factory catalog unverified

## 3. Released reader, documentation, and gates

- [x] 3.1 Add the native Factory released-client witness (design D4) as a
  required gate scenario
- [x] 3.2 Update the Factory harness-validation page to Factory's unverified
  status and registration groundwork
- [x] 3.3 Run `make validate`, `make spec-check`, and `make validate-e2e`
- [x] 3.4 `openspec archive plugin-native-factory --yes`
- [x] 3.5 Amend the proposal, design, tasks, and Factory validation page with
  the CI outcome; re-archive this change
