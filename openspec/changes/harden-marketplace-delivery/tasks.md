## 1. Regression Coverage

- [x] 1.1 Add scanner, web-runtime, and real Helm-rendering regressions and
  observe failures before applying the fixes.

## 2. Delivery Hardening

- [x] 2.1 Simplify scanner failure handling.
- [x] 2.2 Require explicit Helm image versions and update CI fixtures.
- [x] 2.3 Separate builder/runtime images and propagate the runtime override.
- [x] 2.4 Document verified production digest pins, including mirror overrides.
- [x] 2.5 Add Helm and real web-runtime checks to CI.

## 3. Verification

- [x] 3.1 Run scoped tests, container/Compose checks, and Helm validation.
- [ ] 3.2 Verify base images and run the real container smoke checks.
- [ ] 3.3 Run make validate and spec-check, review all changes with OCR
  delegation, and archive the completed change.

## Verification Notes

- Passed: scoped CI-plan tests with real Helm rendering, container static and
  Compose configuration checks, both Helm validation/packaging scripts, the
  witnessed reverse-proxy gate, focused deployment E2E checks, spec-check,
  shellcheck of the new runtime smoke script, Go vet, lint, and build.
- Scoped `make validate TEST_PACKAGES=./tools/ci-plan TEST_FLAGS=-count=1`
  passed. The full short-mode root run with `RUBYOPT=-W0` passed all packages
  except one unchanged TUI test's temporary Git directory cleanup; that exact
  test passed when rerun alone. The Ruby option avoids a macOS warning
  contaminating the Homebrew tests' JSON output. Do not report the full gate
  as passing.
- OCR delegation review covered all 12 previewed files, with no skips (100%
  coverage), plus all 11 excluded files as supplemental context. The review
  found the release-chart caller and null frontend digest inheritance; both
  have failing-before/passing-after regressions and fixes.
- The reviewed tracked diff is SHA-256
  `687bcba7a06703530a4aadf2b9b178f848846f76fb2c512ce3a3f999450b81af`.
  New tests and OpenSpec artifacts were reviewed separately.
- Registry metadata was verified for the Debian Node builder (through an
  approved mirror) and the public distroless runtime, including the runtime's
  Node entrypoint and non-root default. A digest-pinned local web-image build
  was attempted but blocked by Docker Desktop's registry allowlist. No policy
  bypass was attempted. Actual container startup and healthcheck validation
  remain pending the container CI job; task 3.2 intentionally stays unchecked.
