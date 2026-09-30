## 1. Proof format

- [x] 1.1 Define and test the spec's version-1 length-prefixed source-hook fingerprint; add independently calculated vectors at `internal/materialize/testdata/hook-source-fingerprint-v1.json` for each source layout, repository/machine scopes, changed definition/support/mode, and different IR provenance. Add the separate `hook-source/mirror/v1` vectors at `internal/materialize/testdata/hook-source-fingerprint-mirror-v1.json` for verified content-digest identity, changed/missing digest, and domain separation. Verify this release reader consumes both vector families; PR #403's native writer must consume the same committed vectors before its released-client gate.
- [x] 1.2 Add an optional source-hook digest to hook ownership entries and a read-only proof accessor. Test old state remains readable, missing/invalid digests never establish compatibility, and state never contains command/prompt/support-file plaintext.

## 2. Legacy client tolerance

- [x] 2.1 Add read-only native-file inspection for Claude Code, Cursor, Factory Droid and Windsurf in both scopes. Test strict envelope/duplicate-key/path refusal, canonical JSON equivalence, and no ownership-state or aggregate writes during inspection.
- [x] 2.2 Write red consumer tests for each target and scope proving an ownership-clean native entry with matching source fingerprint prevents a legacy duplicate and false drift under `sync`, `sync --locked` and `sync --check`; implement narrow per-hook suppression and keep valid siblings, including Windsurf machine-scope native and legacy entries coexisting in the same file and verified Marketplace mirror hook selection with no original source tree.
- [x] 2.3 Test absent/different/unproven or handwritten native entries preserve legacy output without adopting or pruning native bytes; test unverified or changed mirror digest retains legacy output, and malformed native input leaves both files unchanged while a separate target proceeds.

## 3. Compatibility and publication

- [x] 3.1 Add an isolated mixed-version fixture of the future native file plus ownership proof; run the candidate as legacy-writing client after a simulated native migration, including `sync --locked` and `sync --check`, and require every scenario to execute.
- [ ] 3.2 Annotate every new OpenSpec scenario with `// spec:` test witnesses. Run focused tests, `make validate`, applicable `make validate-e2e`, `make spec-check`, and `openspec validate --all --strict`; fix failures without weakening a gate.
- [x] 3.3 Run `openspec archive hook-native-location-tolerance --yes` in the same PR; verify strict OpenSpec validation and `make spec-check` after archive.
