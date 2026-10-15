## 0. Preconditions

- [ ] 0.1 Re-verify the plugin layout and native registration facts against
  current Claude Code, Factory Droid, Codex, and Agent Plugins documentation,
  and record first-party refs for each catalog's `evidence`
- [ ] 0.2 Add the plugin terms (plugin, plugin member, package format, native
  plugin catalog, package generation, plugin instance, native registration)
  to the domain glossary in `openspec/config.yaml`, and add plugin to the
  definition of Resource

## 1. Plugin catalogs (pkg/harness-compilation)

- [ ] 1.1 Write `pluginpolicy/formats/*.yaml` for the seven formats and
  `pluginpolicy/catalogs/<harness>.yaml` for the seven harness targets per
  design D2, with `delivery.native.status: unverified` everywhere
- [ ] 1.2 Add the strict embedded loader and validation. Write red tests
  first for each rejected case in design D2, annotated
  `plugin-policy-catalogs/malformed-plugin-catalogs-fail-the-check`,
  `plugin-policy-catalogs/verified-native-delivery-without-a-probe-fails-the-check`,
  `plugin-policy-catalogs/verified-native-delivery-for-an-unaccepted-format-fails-the-check`,
  and `plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog`
- [ ] 1.3 Add the probe identifier set to `tools/harness-compat` and its test
  validating the shipped catalogs (design D3)
- [ ] 1.4 Add the validated package layout beside `NativeLayout` with
  failing-case tests; `go test ./pkg/harness-compilation/target -count=1`
- [ ] 1.5 Extend `make catalog-check`, `make catalog-docs` (generating
  `site/docs/reference/plugin-support.md`), and the nightly audit sources.
  Annotate `plugin-policy-catalogs/stale-plugin-support-reference-fails-the-check`.
  Add the plugin recipe to `pkg/harness-compilation/README.md`

## 2. Retire plugin-manifest output (design D5)

- [ ] 2.1 Write red tests for the four `materialization` scenarios of this
  change, then stop the Claude compiler emitting the manifest fragment and
  drop the Claude aggregates and `.claude-plugin`/`.factory-plugin` claims
- [ ] 2.2 Retire earlier managed entries on `sync` and report them on
  `sync --check`; update goldens and affected tests
- [ ] 2.3 Update docs that describe `.claude/.claude-plugin/plugin.json`

## 3. Factory root skill warning (design D6)

- [ ] 3.1 Write red tests for both `harness-compilation` scenarios of this
  change, then add `ingest.factory.root-skill-ignored` to the Factory plugin
  form and the diagnostic catalog

## 4. Gates

- [ ] 4.1 Run `make catalog-check`, `make spec-check`, `make validate`, and
  `make validate-e2e`
