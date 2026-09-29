## 1. Reproduce and fix the discovery regression

- [x] 1.1 Extend the covered-organizational-sub-bundle catalog test with malformed `metadata.requires`; assert the skill and sibling remain discovered, the coded warning is present, and `catalog.discover.sub-bundles-ignored` is absent. Bind `resource-prerequisites/covered-sub-bundle-with-malformed-prerequisite`. Run the test on candidate ac740644 first and observe a failure for the false discovery warning.
- [x] 1.2 Exclude only `catalog.requires.invalid` from discovery-boundary warnings and verify the targeted test passes while `TestDiscover_SubBundlesIgnored*` still distinguishes genuinely ignored sub-bundles.

## 2. Verify and archive

- [x] 2.1 Run `make spec-check` and `make validate` and verify the new scenario binding and affected Go tests pass; the parent will run the full E2E and native gates on the final clean commit.
- [x] 2.2 Run `openspec archive preserve-prerequisite-discovery --yes` as the final commit, then verify the synced main spec and `make spec-check` pass.
