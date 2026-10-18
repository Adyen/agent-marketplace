## 1. Native plugin catalogs

- [ ] 1.1 Add the `claude-marketplace`, `factory-marketplace`, and
  `codex-marketplace` decoders in `plugindecode` (design D3), including
  Claude entry overlay semantics, refused entry source types, and duplicate
  entry names; annotate the catalog-entry scenarios of `plugin-packages`

## 2. Configuration and selection

- [ ] 2.1 Parse `format`, plugin locators, the structured entry, and the
  `delivery`/`exclude` options (design D1). Write red tests for every
  configuration scenario of `plugin-packages`
- [ ] 2.2 Keep plugin-shaped sources without `format` on legacy ingestion;
  annotate `plugin-packages/plugin-shaped-source-without-format-stays-loose`
  with a byte-compare of output and lock against the base
- [ ] 2.3 Resolve plugins with selection edges (direct, collection,
  plugin-to-member) and collection `plugin` members; refuse direct member
  selection and unknown exclusions; keep same-named members distinct and
  undeduplicated (design D2)
- [ ] 2.4 Make the v4 editor round-trip plugin requests, exclusions, and the
  structured entry

## 3. Locks and source revisions

- [ ] 3.1 Add the `plugin-v1` source-revision contract and plugin records
  inside v2 locks (design D4); annotate
  `plugin-packages/plugin-free-layers-keep-their-lock-bytes` and
  `plugin-packages/plugin-selecting-layers-keep-the-v2-lock-format`
- [ ] 3.2 Lock external catalog entries to their own commits, enforce source
  policy per origin before acquisition, and detect stale entries in
  `sync --check` and `sync --locked`; annotate the remaining lock scenarios
- [ ] 3.3 Skip plugin member delivery in sync with a
  `plugin.delivery.pending` warning (design D5)
- [ ] 3.4 Add `plugin-packages/released-v4-client-refuses-a-plugin-configuration`
  to the released-client gate as a required witness

## 4. Gates

- [ ] 4.1 Run `make validate`, `make spec-check`, and `make validate-e2e`
