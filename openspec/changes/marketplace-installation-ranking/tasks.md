## 1. Prove current adoption

- [x] 1.1 Add regression tests for latest-inventory freshness, removed resources, duplicate reports, origin matching, and ambiguity; verify the focused tests fail on the base before implementation.
- [x] 1.2 Implement the bounded inventory query and aggregate counting using existing telemetry; verify the adoption and identity tests pass.

## 2. Integrate discovery

- [x] 2.1 Add optional operator metrics configuration and a bounded server-only five-minute aggregate cache; verify configuration, request-boundary, failure, and expiration tests pass.
- [x] 2.2 Add opt-in installation ordering before search pagination and optional counts while preserving legacy requests; verify ranking, relevance, ties, filters, pagination, and invalid-sort handler tests pass.
- [x] 2.3 Update the API schema and generated frontend types and validation; verify API drift and invalid/absent/zero count tests pass.
- [x] 2.4 Request installation ordering for every browser search page and render optional counts; verify component tests and the filtered incremental-loading browser journey pass.

## 3. Verify and publish claims

- [ ] 3.1 Bind every added scenario to an observable test and reconcile operator/API documentation with reported-adoption limitations; verify spec trace and documentation review.
- [ ] 3.2 Run affected Go checks, frontend checks/build/browser tests, native validation and relevant E2E checks; capture search benchmark comparison and verify all required gates pass.
- [ ] 3.3 Run `openspec archive marketplace-installation-ranking --yes` in the same pull request and verify strict archived/main-spec validation.
