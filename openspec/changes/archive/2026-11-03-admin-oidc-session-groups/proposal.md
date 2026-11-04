## Why

Admin OIDC sign-in fails for operators whose identity provider returns many groups (issue #964, `agent-marketplace-uxoj`). The callback stores every ID-token group in the signed session cookie, so the sealed cookie grows with the user's group count. Behind ingress-nginx's default 4k proxy buffer the callback's response headers overflow and the request returns 502, and when the sealed value exceeds the 3900-byte cookie bound the callback fails with a generic 401 that names no cause.

## What Changes

- The administration session stores only the verified ID-token groups that match `admin_groups`. Access granted only through `admin_users` stores no groups. Session cookie size then depends on operator configuration, not on the user's IdP group count.
- Per-request administration authorization is unchanged: a session is admitted when its stored groups intersect `admin_groups`, or when its subject is listed in `admin_users`.
- When the session cannot be sealed within the cookie bound, the callback logs `outcome=session_too_large` with no subject or group values, sets no session cookie, and keeps the existing `401 authentication_rejected` response.
- Reverse-proxy documentation states the default 4k header-buffer requirement and that session cookie size no longer scales with IdP group count.
- `make marketplace-proxy-check` gains a callback case with a 256-group token under the default proxy buffer that expects `303`, not `502`. It runs in Docker and stays outside the ordinary test target.

**Not changed:** the login transaction cookie, PKCE, nonce, state, ID-token validation, session cookie name, attributes, lifetime, and payload format, `maxAdminCookie`, `maxAdminGroups`, the trusted-proxy identity mode, session payload encryption, and ingress-nginx defaults.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-admin-auth`: adds the stored-session contents requirement and the `session_too_large` callback outcome.

## Impact

- `pkg/marketplace/api/admin_oidc.go`: narrow stored groups in the callback; log the size outcome in the seal-failure path.
- `pkg/marketplace/api/admin_oidc_test.go`: new and rewritten callback cases (see tasks).
- `docs/marketplace-reverse-proxy.md` and `site/docs/how-to/operator/configure-a-reverse-proxy.md`: header-buffer requirement.
- `tools/marketplace-proxy/`: 256-group callback case for the Docker proxy gate.
- No API, configuration schema, cookie format, or dependency changes. Existing sessions keep working unchanged.

## Release note

Admin OIDC session size no longer scales with unmatched IdP groups. The session stores only groups listed in `admin_groups`; a session that still cannot be sealed logs `outcome=session_too_large`. Superseded by `admin-oidc-session-index`, which replaces stored group names with a bitset over `admin_groups`. Reverse proxies in front of native OIDC administration need a response-header buffer of at least 4096 bytes.
