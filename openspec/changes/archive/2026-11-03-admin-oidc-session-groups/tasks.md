## 1. Session narrowing and size outcome

- [x] 1.1 Red: add `pkg/marketplace/api/admin_oidc_test.go` cases asserting a 256-group token with one `admin_groups` match stores exactly that group (AC1), a `admin_users`-only identity stores no groups (AC3), and the callback response header block for a 256-group `admin_groups` authorization is under 4096 bytes (AC2). Verify: `make test TEST_PACKAGES=./pkg/marketplace/api TEST_FLAGS="-run Admin -count=1"` fails for the intended reason.
- [x] 1.2 Store only `groups ∩ admin_groups` in the callback session payload; keep `adminAuthorized` unchanged. Verify: the 1.1 cases pass and existing `admin_oidc_test.go` cases still pass.
- [x] 1.3 Red: add a case where the `admin_groups` intersection exceeds the cookie bound, asserting `outcome=session_too_large` logged without subject or group values, no session cookie, and `401 authentication_rejected` (AC4). Replace the existing `oversized verified groups rejected without session` subtest, which depended on storing every group. Verify: the case fails before the fix.
- [x] 1.4 Add a sentinel for the cookie-bound failure from `seal()` and log `outcome=session_too_large` in the callback seal-failure path, keeping `401 authentication_rejected`. Verify: 1.3 passes.

## 2. Specs

- [x] 2.1 Annotate every new or rewritten scenario test with `// spec: marketplace-admin-auth/<scenario-slug>`. Verify: `go run ./tools/specaudit trace` reports no unbound scenarios for this change.
- [x] 2.2 Archive the change through the documentation gate and pass `make spec-check`. Verify: `make spec-check` exits 0.

## 3. Reverse-proxy documentation

- [x] 3.1 State the default 4k header-buffer requirement and that session cookie size no longer scales with IdP group count in `docs/marketplace-reverse-proxy.md` and `site/docs/how-to/operator/configure-a-reverse-proxy.md`. Verify: the text matches the narrowed behavior and cites the 4096-byte budget.

## 4. Proxy gate case

- [x] 4.1 Add a callback case to `tools/marketplace-proxy` with a 256-group token under the default proxy buffer that expects `303`, not `502`. Verify: `make marketplace-proxy-check` passes in Docker (outside the ordinary test target); if Docker is unavailable, record the environment blocker and do not claim the case passed.
