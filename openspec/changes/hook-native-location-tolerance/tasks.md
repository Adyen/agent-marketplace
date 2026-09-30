## 1. Proof format

- [ ] 1.1 Define and test a versioned source-hook fingerprint over stable original source bytes, support files, source identity, installed name, target and scope; verify two clients with different IR provenance yield the same digest and changed matcher/command/support bytes do not.
- [ ] 1.2 Add an optional source-hook digest to hook ownership entries and a read-only proof accessor. Test old state remains readable, missing/invalid digests never establish compatibility, and state never contains command/prompt/support-file plaintext.

## 2. Legacy client tolerance

- [ ] 2.1 Add read-only native-file inspection for Claude Code, Cursor, Factory Droid and Windsurf in both scopes. Test strict envelope/duplicate-key/path refusal, canonical JSON equivalence, and no ownership-state or aggregate writes during inspection.
- [ ] 2.2 Write red consumer tests for each target and scope proving an ownership-clean native entry with matching source fingerprint prevents a legacy duplicate and false drift under `sync`, `sync --locked` and `sync --check`; implement the narrow per-hook suppression and keep valid siblings.
- [ ] 2.3 Test absent/different/unproven or handwritten native entries preserve legacy output without adopting or pruning native bytes; test malformed native input leaves both files unchanged and a separate target proceeds.

## 3. Compatibility and publication

- [ ] 3.1 Add an isolated mixed-version fixture of the future native file plus ownership proof; run the candidate as legacy-writing client after a simulated native migration, including `sync --locked` and `sync --check`, and require every scenario to execute.
- [ ] 3.2 Annotate every new OpenSpec scenario with `// spec:` test witnesses. Run focused tests, `make validate`, applicable `make validate-e2e`, `make spec-check`, and `openspec validate --all --strict`; fix failures without weakening a gate.
- [ ] 3.3 Run `openspec archive hook-native-location-tolerance --yes` in the same PR; verify strict OpenSpec validation and `make spec-check` after archive.
