## 1. Catalog

- [x] 1.1 Add per-scope native status to the plugin catalog schema and
  validation (design D3); annotate
  `plugin-policy-catalogs/a-scope-declared-verified-without-a-probe-fails-the-check`
- [x] 1.2 Declare Claude native delivery verified at user scope and unverified
  at repository scope; regenerate the plugin support reference with
  per-scope status; annotate
  `plugin-policy-catalogs/the-reference-shows-differing-scope-statuses`

## 2. Delivery

- [x] 2.1 Make delivery resolution honor the request's scope; annotate
  `plugin-delivery/auto-delivers-claude-plugins-natively-at-user-scope-only`
  and `plugin-delivery/explicit-native-delivery-is-refused-at-repository-scope-for-claude`

## 3. Probe

- [x] 3.1 Reduce the Claude probe to the user-scope positive witness (design
  D1, D2) and the repository-scope negative witness (design D4); remove
  experiment-only code; annotate the three probe scenarios

## 4. Documentation and gates

- [x] 4.1 Update `docs/harness-validation/claude.md` with user-only
  verification and the open repository-scope gap
- [x] 4.2 Run `make validate`, `make spec-check`, and `make validate-e2e`
- [x] 4.3 Archive the change
