## 1. Registration shape

- [x] 1.1 Write Codex catalog entries with relative `local` sources, policy,
  and category over linked generations (design D1)
- [x] 1.2 Write the Codex marketplace registration with `source_type` and
  `source`, replacing owned entries of the earlier shape in one sync (design
  D2); annotate
  `plugin-delivery/codex-registration-preserves-handwritten-config`
- [x] 1.3 Cover deselection removing the Codex catalog entry and link, and
  unowned look-alike entries staying unchanged

## 2. Shape probe and native status

- [x] 2.1 Add the Codex shape probe to the Codex harness-compatibility job
  with a negative control before sync (design D3); annotate
  `plugin-delivery/codex-catalog-is-a-marketplace-codex-accepts`
- [x] 2.2 Keep Codex native delivery `unverified`; keep `auto` decomposing
  Codex plugins and explicit `delivery: native` refused

## 3. Released reader, documentation, and gates

- [x] 3.1 Add the native Codex released-client witness (design D4) as a
  required gate scenario
- [x] 3.2 Record the observed Codex plugin behavior on the Codex
  harness-validation page
- [x] 3.3 Run `make validate`, `make spec-check`, and the focused Codex e2e
  tests
- [ ] 3.4 `openspec archive plugin-native-codex --yes`
