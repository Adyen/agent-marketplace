# Native OIDC authentication for Marketplace administration

**Status:** Approved in conversation; awaiting written-design review before implementation.
**Date:** 2026-09-19.
**Baseline:** `admin-interface` at `ad3ba170d3f227fa3bc8234c7a64fd40f68a9eac`.
**Audience:** Marketplace maintainers, operators, security reviewers, and deployment owners.

## 1. Purpose and scope

The hosted Marketplace administration interface currently accepts identity only from a trusted reverse proxy. Production should instead be able to authenticate administrators directly with a dedicated Okta OpenID Connect application while preserving the existing authorization and audit path.

This change is confined to the administration edge:

- add login, callback, logout, and a signed application session;
- validate a dedicated public OIDC client's authorization-code flow with S256 PKCE;
- authorize verified identities by configured Okta groups or immutable Okta subjects;
- convert the verified identity to the existing `store.ManagedActor` used by administration services;
- retain trusted-proxy authentication as an optional deployment mode;
- add explicit sign-in and logout behavior to the existing Nuxt admin page.

Public catalog, search, artifacts, workers, ingestion, managed configuration, metadata storage, blob storage, and ordinary Marketplace pages remain anonymous and unchanged. This design does not create a general user-authentication subsystem.

## 2. Decisions and alternatives

### 2.1 Dedicated Okta application

Marketplace uses its own Okta OIDC application rather than reusing a client ID from a prior reference service. This gives Marketplace an independent redirect URI, audience, assignment policy, claims contract, and revocation boundary.

The application is a public client: client authentication is disabled and S256 PKCE is required. Marketplace stores no OIDC client secret.

### 2.2 Signed stateless session

Three session approaches were considered:

| Approach | Decision |
|---|---|
| Signed, stateless, host-only cookie | Selected. It matches the deployment model of a prior reference service, works across replicas with one shared secret, and adds no session table or cleanup job. |
| Database-backed sessions | Rejected for this slice. Immediate server-side revocation does not justify adding persistence and cleanup to every admin request. |
| Browser-held OIDC tokens | Rejected. It exposes provider tokens to frontend JavaScript and complicates renewal and logout. |

The session is application-managed but not database-resident. Local logout expires the cookie. Okta group changes take effect when the bounded session expires; Marketplace does not query Okta on every request.

### 2.3 Group-or-subject authorization

A verified identity is authorized when either:

- at least one bounded string in the configured ID-token groups claim exactly matches `admin_groups`; or
- the immutable verified ID-token `sub` exactly matches `admin_users`.

`admin_users` never matches email or `preferred_username`. Those mutable claims may be displayed but are not authorization or audit identity. The durable actor is:

```text
ManagedActor{Issuer: <verified OIDC issuer>, Subject: <verified sub>}
```

### 2.4 Shared authorization guard

OIDC sessions and trusted-proxy claims are provider-specific identity inputs. Both converge before the existing request protections and administration handlers. The implementation must not duplicate origin, content-type, request-size, rate-limit, or actor-context policy per provider.

## 3. Configuration contract

Administration selects one authentication mode:

```yaml
admin:
  enabled: true
  origin: https://marketplace.example

  authentication:
    mode: oidc
    oidc:
      issuer: https://idp.example.com/oauth2/default
      client_id: MARKETPLACE_OKTA_CLIENT_ID
      session_secret_env: MARKETPLACE_ADMIN_SESSION_SECRET
      groups_claim: groups
      session_ttl: 12h

  admin_groups:
    - marketplace-admins
    - another-marketplace-admin-group

  admin_users:
    - 00u123exampleOktaSubject
```

`admin.origin` is validated in canonical form and must equal the browser's exact origin serialization: lowercase scheme, ASCII DNS host (Punycode A-label), or literal IP host; no explicit default port (`:443` for HTTPS or `:80` for HTTP), no leading-zero port, no trailing-dot DNS host, no non-canonical IPv4 spelling (decimal, hex, dotted octal/hex/leading-zero forms, or IPv4-mapped IPv6 such as `[::ffff:192.0.2.1]` — operators must use the canonical IPv4 literal), and no path, query, or fragment. Canonical HTTPS origins and explicit non-default loopback HTTP origins under the localhost opt-in remain valid.

The client ID is not secret and may be stored directly in deployment configuration. The session signing secret is accepted only by environment or file reference and must resolve to 32 through 4096 bytes after whitespace trimming. Inline session secrets are not supported.

The default session lifetime is 12 hours. Configuration enforces a non-negative bounded lifetime no greater than 24 hours; a zero or omitted value applies the 12-hour default. The transient login transaction lasts 10 minutes and is not configurable in the first release.

`groups_claim` defaults to `groups` and must be a single safe claim name. Requested scopes are fixed to `openid profile email groups`; deployments cannot omit `openid`, add arbitrary scopes, or request offline access through this configuration.

Trusted-proxy mode remains available:

```yaml
admin:
  enabled: true
  origin: https://marketplace.example
  authentication:
    mode: trusted_proxy
  trusted_proxies: [127.0.0.1/32]
  issuer: deployment-sso
  subject_header: X-Marketplace-Subject
  groups_header: X-Marketplace-Groups
  admin_groups: [marketplace-admins]
```

For compatibility, an enabled existing configuration with no `authentication.mode` and a complete set of proxy fields infers `trusted_proxy`. New OIDC configuration rejects proxy identity fields. Trusted-proxy mode rejects OIDC fields. Empty authorization lists are invalid in both modes: at least one admin group or admin subject must be configured. `allow_insecure_localhost` remains an explicit loopback-only development option and never permits non-loopback HTTP.

Invalid administration authentication configuration fails the admin routes closed. It does not prevent public Marketplace routes from serving.

## 4. HTTP and OIDC flow

### 4.1 Login

`GET /api/v1/admin/auth/login?next=<path>` performs these steps:

1. Require enabled OIDC administration.
2. Validate `next`. The only accepted destinations are `/marketplace/admin` and descendants. Empty input uses `/marketplace/admin`.
3. Generate cryptographically random state, nonce, and PKCE verifier.
4. Compute the S256 code challenge.
5. Store the versioned state, nonce, verifier, validated destination, issued-at, and expiry in a signed transient cookie.
6. Redirect to the discovered authorization endpoint with the exact configured client ID, callback URI, scopes, state, nonce, challenge, and `code_challenge_method=S256`.

Absolute URLs, protocol-relative paths, unrelated local paths, backslashes, control characters, and encoded path-confusion forms are rejected. Marketplace never reflects an unvalidated destination into a redirect.

### 4.2 Callback

`GET /api/v1/admin/auth/callback`:

1. Loads and verifies the transient cookie and its expiry.
2. Requires exactly one bounded `state` and authorization `code` parameter and rejects provider error responses generically.
3. Compares state in constant time.
4. Expires the transient cookie before attempting exchange.
5. Exchanges the code at the discovered token endpoint using the original verifier and callback URI, with no client secret.
6. Requires an ID token and validates its signature and claims.
7. Authorizes its verified groups or subject.
8. Issues the signed admin session and redirects to the validated destination.

The ID token validation contract requires:

- signing algorithm `RS256`;
- signature from the issuer's current JWKS;
- exact configured issuer;
- audience containing the configured client ID;
- `azp`, when present, a single bounded string equal to the configured client ID, and required when the token carries multiple audiences;
- valid expiry and issued-at with a small bounded clock skew;
- nonce equal to the transaction nonce;
- nonempty bounded `sub`;
- a bounded groups claim containing only bounded strings when present.

Access tokens, refresh tokens, ID tokens, authorization codes, verifier material, and raw provider responses are discarded after callback processing and never stored in cookies, logs, errors, or audit records.

### 4.3 Session use

The signed session cookie contains only a version, issuer, subject, bounded groups, optional bounded display name, issued-at, and expiry. It is HMAC-SHA-256 signed and verified with constant-time comparison.

Every admin API request re-evaluates the signed subject and groups against the current deployment-configured `admin_users` and `admin_groups`. A valid session that no longer matches returns `403 forbidden`. A missing, malformed, tampered, or expired session returns `401 unauthenticated` and is never refreshed implicitly.

The cookies are `HttpOnly` and `SameSite=Lax` with no `Domain` attribute. For HTTPS deployments both use the browser-enforced `__Host-` names `__Host-marketplace_admin_session` and `__Host-marketplace_admin_login` with `Secure` and `Path=/`; the prefix contract forbids `Domain` and requires `Path=/`, so a same-named cookie set by a sibling domain can never collide with or overwrite the admin cookies. This deliberately trades per-path confinement for stronger host isolation enforced by the browser rather than by attribute matching. Only the explicit loopback HTTP development option keeps the unprefixed names `marketplace_admin_session` and `marketplace_admin_login` without `Secure`, retaining path scopes `/api/v1/admin` and `/api/v1/admin/auth`. Request lookup and expiration always resolve the effective mode-specific name and path.

Both signed cookies share one sealed-value bound of 3900 bytes so the complete `Set-Cookie` header line, including the longest cookie name and attribute margin, stays below the 4096-byte browser delivery limit. A verified, authorized identity whose claims cannot fit within that bound is rejected with the generic `401 authentication_rejected` and receives no session cookie; it is not an infrastructure failure.

### 4.4 Logout

`POST /api/v1/admin/auth/logout` uses the shared mutation protections and expires the local session cookie. It is idempotent and does not contact Okta or perform global logout.

## 5. Shared request security

After provider-specific identity extraction, all authenticated administration requests retain the existing controls:

- exact same-origin validation for mutations and any request carrying `Origin`;
- required `X-Marketplace-Admin: 1` for mutations;
- JSON-only mutation content type;
- one-megabyte mutation body limit;
- per-actor and global rate limits, with stricter preview limits;
- no CORS response;
- `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`;
- no automatic mutation replay;
- one `ManagedActor` context consumed by current handlers and audit persistence.

Login is a GET because it creates only a short-lived browser-local transaction and redirects to the identity provider. Callback login CSRF is prevented by the signed transaction, state, nonce, and PKCE. Logout is a protected POST.

## 6. Provider and dependency boundary

The implementation uses established Go OIDC and OAuth libraries rather than handwritten JWT validation. The intended dependencies are `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2`, using versions that satisfy the repository's dependency-age policy.

A focused provider interface separates protocol behavior from HTTP handlers and permits an in-process issuer in tests. Production behavior includes:

- lazy discovery so temporary Okta unavailability does not stop public server startup;
- proxy-aware outbound HTTP transport so `HTTPS_PROXY` works in production;
- bounded request timeouts;
- cached discovery and JWKS;
- one JWKS refresh when an otherwise valid token references an unknown key ID;
- no arbitrary issuer endpoints or callback URI accepted from requests.

The callback URI is derived from the validated configured origin and the fixed path `/api/v1/admin/auth/callback`. It is not separately configurable, preventing origin/callback drift.

## 7. Frontend behavior

The Nuxt admin page remains client-only. Its initial session request continues to be the authentication probe.

The session response and `401 unauthenticated` error metadata expose the resolved authentication mode. In OIDC mode, the page shows a **Sign in with Okta** action whose `next` parameter is the current validated admin route. In trusted-proxy mode, it instead directs the user to reauthenticate through the deployment identity proxy. It does not automatically redirect: explicit navigation avoids redirect loops and preserves an understandable failure state.

On `403 forbidden`, the page states that the authenticated identity lacks Marketplace administration permission and does not offer mutation controls.

The authenticated header shows the optional display value while audit identity remains issuer plus subject. In OIDC mode it provides an explicit logout action. Logout uses the same-origin admin API helper and mutation headers, clears local page state, and returns to the signed-out view. Trusted-proxy sessions do not offer a local logout control because authentication lifecycle belongs to the deployment proxy.

A failed request or expired session never causes the frontend to replay a mutation. Drafts remain in the browser tab and may be submitted only after a fresh explicit user action.

## 8. Errors, logging, and observability

Stable external outcomes are:

| Condition | Status/code |
|---|---|
| Missing, invalid, or expired session | `401 unauthenticated` |
| Verified identity lacks configured authorization | `403 forbidden` |
| Invalid login destination or callback shape | `400 authentication_request_invalid` |
| Invalid state, token, signature, or claims | `401 authentication_rejected` |
| Discovery, JWKS, or token endpoint unavailable | `503 authentication_unavailable` |
| OIDC mode not configured safely | `503 admin_unavailable` |

Responses are generic and never contain provider response bodies or claim values. Structured logs record operation boundaries and low-cardinality outcome codes without subject, email, groups, token, cookie, authorization code, verifier, or upstream payload. Metrics, if added, use only provider mode and bounded outcome labels.

Public availability does not depend on Okta. OIDC failures affect login and OIDC-authenticated admin requests only.

## 9. Test strategy and accepted seams

Tests exercise observable behavior through agreed public seams.

### Configuration seam

YAML decoding and `Administration.Validate()` cover:

- OIDC and trusted-proxy valid controls;
- backward inference of proxy mode;
- rejection of mixed or incomplete modes;
- secret reference and resolved-secret bounds;
- authorization-list and TTL bounds;
- HTTPS and explicit loopback HTTP policy.

### HTTP authentication seam

`Server.PublicHandler()` is exercised against an in-process OIDC issuer using real RSA-signed test ID tokens. Coverage includes:

- authorization redirect parameters and safe `next` handling;
- state, PKCE, nonce, callback exchange, issuer, audience, time, and algorithm validation;
- session cookie attributes and tamper/expiry handling;
- group authorization, subject authorization, and unauthorized identities;
- bounded claims and cookies;
- signing-key refresh;
- safe provider failures and redacted responses;
- local logout.

### Existing admin API seam

A session-authenticated request reaches `/api/v1/admin/session` and a representative mutation with the exact immutable OIDC `ManagedActor`. Existing trusted-proxy authorization tests continue to pass without duplicating shared guard expectations.

### Browser seam

Unit and Playwright tests cover explicit sign-in, forbidden/error presentation, logout, current-route `next`, and the absence of automatic mutation replay.

Tests do not target private cryptographic helper implementation details. Test fixtures never call external Okta services and never read developer credentials or home-directory state.

## 10. Implementation slices

Implementation proceeds as test-first vertical slices:

1. Authentication-mode configuration and secret resolution.
2. OIDC login redirect with safe destination and signed transaction.
3. Callback validation, authorization, and signed session.
4. Provider-independent shared admin guard and `ManagedActor` convergence.
5. Logout and Nuxt signed-in/signed-out behavior.
6. OpenAPI and operator documentation.
7. Full application verification and security-oriented diff review.

Expected changes are concentrated in `pkg/marketplace/config`, `pkg/marketplace/api`, the existing admin Nuxt page/client/tests, `api/openapi.yaml`, operator documentation, and Go module metadata. No worker, ingestion, store schema, public catalog route, or artifact behavior is in scope.

## 11. Verification and rollout

Application verification requires:

- focused configuration and API package tests;
- complete `go test ./...`;
- `make ci-lint`;
- `make build`;
- all web unit tests and frontend typecheck/build;
- focused Playwright admin coverage;
- Compose regression in the existing trusted-proxy local mode;
- final review for cookie/token/claim leakage and public-route regressions.

Repository security policy, including dependency minimum-release-age controls, is not weakened to make verification pass.

The dedicated Okta application contract is:

- public Authorization Code client;
- client authentication disabled;
- S256 PKCE required;
- redirect URI `https://marketplace.example/api/v1/admin/auth/callback`;
- scopes `openid profile email groups`;
- RS256 ID-token signing;
- standard immutable `sub` claim;
- `groups` ID-token claim as an array of strings.

Production GitOps wiring follows application verification and remains a separate declarative change. It supplies the issuer, client ID, Vault-backed session secret, groups and subjects, proxy egress, immutable application image, and database authority configuration. It removes the need for trusted identity headers, trusted proxy ranges, group-header conversion, or an OAuth2-proxy bridge.

Deploying the image and configuration does not perform database adoption. Adoption remains a separately approved maintenance operation with coordinated metadata/blob backups and rollback.

## 12. Explicit non-goals

This change does not provide:

- authentication for public Marketplace browsing or consumer CLI APIs;
- refresh tokens, offline access, or silent browser renewal;
- global Okta logout;
- database-backed session revocation;
- arbitrary roles or per-operation permissions;
- an admin-user editor in the admin interface;
- live Okta group lookup on every request;
- OAuth2-proxy or ingress-header setup;
- automatic cluster apply, Argo sync, or production adoption.
