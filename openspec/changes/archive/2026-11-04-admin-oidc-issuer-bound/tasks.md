## 1. Issuer bound

- [x] 1.1 Red: add an over-512-byte issuer row to `TestAdministrationOIDCValidation` in `pkg/marketplace/config/administration_test.go`, annotated `// spec: marketplace-admin-auth/issuer-longer-than-512-bytes-is-refused-at-configuration`. Verified: it failed before the change (`unsafe admin config accepted`).
- [x] 1.2 Add `maxAdminIssuer = 512` and the length check in `validateOIDC`. Verified: `TestAdministrationOIDC` passes.

## 2. Specs

- [x] 2.1 Update the `session_too_large` scenario annotation and wording; the test is `TestAdminOIDCCookieBrowserDeliveryBound` and keeps its existing `// spec:` line.
- [ ] 2.2 Run `make spec-check` and archive the change.
