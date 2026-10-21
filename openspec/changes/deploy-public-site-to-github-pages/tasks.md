## 1. Publication

- [x] 1.1 Add regression tests for main-only native Pages publication and safe validation-only runs.
- [x] 1.2 Configure, validate, upload, and deploy the public site using job-scoped permissions and the configured Pages base path.
- [x] 1.3 Check generated public-site navigation and assets at root and project deployment paths.

## 2. Maintainer Setup

- [x] 2.1 Explain Pages activation, custom-domain verification, DNS, managed HTTPS, and rebuilding after a domain change in the implementation handoff, not a README.

## 3. Verification

- [x] 3.1 Run focused workflow and site tests, `make validate`, and `make spec-check`.
- [x] 3.2 Review the changes with the open-code-review-delegate workflow and resolve actionable findings.
- [ ] 3.3 Run `openspec archive deploy-public-site-to-github-pages --yes` in the same pull request as the implementation.

## Verification Record

- Workflow tests first failed against the build-only workflow, then the complete
  `tools/specaudit` package passed after implementation.
- Public-site builds and all six site tests passed at both
  `/agent-marketplace/` and `/`; documentation-only containment and article
  parity passed in both contexts.
- `make spec-check` passed strict validation, scenario traceability, and archive
  checks.
- `make validate TEST_PACKAGES=./tools/specaudit` passed scoped tests, Go vet,
  module hygiene, the CLI build, and lint with the CI-pinned golangci-lint
  2.14.0 built with Go 1.27.3. The full root test suite was not run locally.
- Site dependencies were installed from the frozen lockfile with pinned pnpm
  10.26.0. The official Pages action tags were verified against their pinned
  commits and their declared inputs and outputs were checked.
- OCR delegation preview, rule resolution, and host review completed for the
  final implementation diff: 2 reviewable files, 2 reviewed, 0 skipped,
  coverage 100%, no actionable findings. The nine additional site, test, and
  Markdown files excluded by OCR defaults were reviewed manually. Concurrent
  unrelated configuration optimization work was excluded.
- No live Pages deployment, repository setting, DNS change, or certificate
  issuance was performed locally.
