## Why

The administration session still stores matching `admin_groups` names, and each name can be up to 512 bytes. An operator whose matching groups are long or numerous can exceed the 3900-byte cookie bound, so sign-in fails with `session_too_large` even though the admin cannot change their group membership. Reviewer findings on PR #1003 also showed that a valid callback can produce a response-header block above the 4096-byte default proxy buffer: at a sealed cookie of about 3850 to 3900 bytes, nginx returns 502 before the response reaches the browser.

## What Changes

- The administration session stores a membership bitset over the configured `admin_groups` list, plus a fingerprint of that list, instead of group names. Cookie size then depends on operator configuration and not on the names or number of IdP groups the user holds.
- A session whose fingerprint does not match the current `admin_groups` list is rejected as unauthenticated, so the operator signs in again after the configuration changes.
- The sign-in destination (`next`) is limited to printable ASCII and 1024 bytes, which bounds the `Location` header of the callback.
- Per-request authorization is unchanged: a session is admitted when its matched groups intersect `admin_groups`, or when its subject is listed in `admin_users`.
- Sessions issued by the previous build fail verification once, so those administrators sign in again after upgrade.

**Not changed:** the login transaction cookie, PKCE, nonce, state, ID-token validation, the session cookie name, attributes, and lifetime, `maxAdminCookie`, `maxAdminGroups`, the session cookie overflow outcome, and the trusted-proxy identity mode.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-admin-auth`: rewrites the stored-session contents requirement, adds the proxy header budget and sign-in destination bounds.

## Impact

- `pkg/marketplace/api/admin_oidc.go`: session payload, callback membership encoding, session decoding and fingerprint check, `next` validation.
- `pkg/marketplace/api/admin_oidc_test.go`: callback, session, and `next` cases.
- `tools/marketplace-proxy/proxy_test.go`: callback case at the realistic maximum sealed size under the default proxy buffer.
- `docs/marketplace-reverse-proxy.md` and `site/docs/how-to/operator/configure-a-reverse-proxy.md`: header-buffer statement.
- No API, configuration schema, or dependency changes. Existing sessions stop verifying once, after upgrade.

## Release note

Admin OIDC sessions now record administration group membership as a compact bitset over the configured `admin_groups`, so sign-in no longer fails for administrators whose matching groups are long or numerous. Sessions issued before this release, and sessions after any change to `admin_groups`, require signing in again. Sign-in destinations must be printable ASCII of at most 1024 bytes, and reverse proxies in front of native OIDC administration keep the default 4 KiB response-header buffer.
