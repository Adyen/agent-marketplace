## 1. Regression test first

- [x] 1.1 Add a source-read-count regression through the real lock path that proves acquisition work can be reused during snapshot binding; confirm it fails against the current repeated-build behavior.

## 2. Reuse operation-local results

- [x] 2.1 Pass the matching source revision and catalog from acquisition to snapshot binding, compare them with the locked projection after exact snapshot cache revalidation, and retain `Build` fallback; verify the regression from 1.1 passes.

## 3. Verify compatibility

- [x] 3.1 Add drift and refusal regressions that prove fresh source verification remains in force and lock and revision bytes stay unchanged; verify those tests pass.
- [x] 3.2 Run `CGO_ENABLED=0 make test TEST_PACKAGES='./internal/sourcerevision ./internal/catalog' TEST_FLAGS='-count=1'` and verify both focused packages pass.

## 4. Finish

- [ ] 4.1 Run `openspec archive reuse-command-source-revisions --yes` and verify the change is archived.
