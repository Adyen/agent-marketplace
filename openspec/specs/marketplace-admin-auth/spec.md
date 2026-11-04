# marketplace-admin-auth Specification

## Purpose
Provides browser-facing failure explanations and explicit recovery for Marketplace OIDC administration sign-in while retaining the existing machine-client error contract and authentication protections.

## Requirements

### Requirement: Browser-aware OIDC failure responses

OIDC login and callback failures SHALL issue a 303 redirect to a same-origin sign-in error page beneath the path component of the validated configured Marketplace browser base only when the request explicitly prefers an acceptable `text/html` response over JSON. A root browser base SHALL resolve that page to `/auth/error`; an explicit non-root base SHALL prefix the same route with its path. When no browser base is configured, the existing `/marketplace/auth/error` fallback SHALL remain. Missing `Accept`, wildcard-only `Accept`, JSON preference, and tied preference SHALL retain existing JSON error bodies and status codes. This policy SHALL cover request-origin and OIDC initialization failures at these endpoints without changing logout or administration data responses.

#### Scenario: Browser and machine clients receive appropriate failure responses

- **WHEN** OIDC login or callback fails due to provider denial, invalid or expired transaction, invalid state or token, malformed response, provider outage, rejected origin, or unavailable authentication configuration
- **THEN** HTML-preferring requests receive a 303 same-origin error-page redirect beneath the configured Marketplace browser-base path
- **AND** root, explicit non-root, and absent browser-base configurations resolve respectively to the root route, prefixed route, and legacy fallback
- **AND** JSON-preferring, missing-`Accept`, and wildcard-only requests retain the corresponding existing JSON error code and HTTP status

### Requirement: Independent sign-in error page

The Marketplace SHALL server-render a dedicated sign-in error page without requesting an administration session or configuration. It SHALL present safe explanations for `authentication_rejected`, `authentication_unavailable`, `forbidden`, `authentication_request_invalid`, `admin_unavailable`, and `origin_rejected`, and a generic fallback for unknown, duplicated, or malformed error parameters.

#### Scenario: Failure page renders without a session

- **WHEN** a user visits a supported sign-in error URL or one with unknown, duplicated, or malformed error parameters
- **THEN** the document contains the corresponding safe explanation or generic fallback without JavaScript, and rendering does not fetch administration session or configuration data.

### Requirement: Explicit safe sign-in recovery

The error page SHALL provide a return link to the configured Marketplace browser base and an explicit fresh-sign-in action for retryable failures. Retry destinations SHALL be limited to validated same-origin administration paths beneath that browser base and otherwise fall back to its administration root. When no browser base is configured, existing `/marketplace` return and administration fallback paths SHALL remain. Authorization or configuration guidance SHALL NOT imply retry grants permission, and no sign-in or administration operation SHALL be retried automatically.

#### Scenario: Recovery starts a new login with a safe destination

- **WHEN** a user follows the recovery action after a retryable failure with a valid administration destination, or with an unsafe or missing destination
- **THEN** a new login transaction starts with the validated destination or the administration-root fallback beneath the configured Marketplace browser-base path
- **AND** root, explicit non-root, and absent browser-base configurations use their corresponding root, prefixed, or legacy return and fallback paths
- **AND** every failure page offers the applicable Marketplace return link without automatically replaying an operation

### Requirement: Preserved authentication and diagnostic privacy

Error presentation SHALL preserve existing provider verification, authorization, transaction consumption, and session-cookie issuance rules. Provider descriptions, state, authorization codes, tokens, and identity claims SHALL NOT propagate into error-page redirects or content. Provider `access_denied` SHALL remain a generic `authentication_rejected` outcome distinct from `forbidden` after verified identity authorization.

#### Scenario: Failed authentication does not leak diagnostics or issue a session

- **WHEN** a failed sign-in includes sensitive provider or callback values, or a verified identity lacks administration permission
- **THEN** error redirects and pages expose none of those sensitive values, the existing failure classification and login-cookie consumption are preserved, and no new administration session is issued.

### Requirement: Private accessible error presentation

Error handoffs and pages SHALL set no-store caching, noindex robot controls, and no-referrer referrer policy. The page SHALL support light and dark appearance, narrow mobile viewports, keyboard navigation, and reduced-motion preferences using the Marketplace visual conventions.

#### Scenario: Error presentation is private and accessible

- **WHEN** a user reaches the sign-in error page in light or dark appearance, on desktop or mobile, using keyboard navigation or reduced motion
- **THEN** the response applies no-store, noindex, and no-referrer, and the explanation and recovery controls remain readable, keyboard accessible, and usable without mandatory motion.

### Requirement: Published browser error contract

The canonical OpenAPI and generated client types SHALL describe browser error redirects while retaining JSON failure contracts. Operator documentation SHALL describe safe browser error recovery.

#### Scenario: Published contract matches browser and JSON behavior

- **WHEN** authentication route definitions and operator guidance are inspected against the implemented sign-in flow
- **THEN** browser failure redirects, retained JSON codes and statuses, and safe retry behavior are documented consistently, and generated client types match the canonical API.

### Requirement: Marketplace browser bases are canonical

A configured Marketplace browser base SHALL be an absolute public URL whose path is canonical and safe to use as a same-origin browser-route prefix. Marketplace operator configuration SHALL reject repeated leading separators, backslashes, control characters, literal or encoded dot segments, and paths containing or requiring percent encoding instead of silently selecting a different or unsupported browser route.

#### Scenario: Unsafe browser bases fail during configuration loading

- **WHEN** a Marketplace operator configures a browser base with a non-canonical or unsafe path
- **THEN** configuration loading MUST fail with an error identifying `instance.browser_base` without exposing URL credentials or query values
- **AND** canonical root and unescaped subpath browser bases MUST remain valid

#### Scenario: Unsupported encoded browser bases fail during configuration loading

- **WHEN** a Marketplace operator configures a browser-base path containing percent escapes, spaces, or non-ASCII characters requiring percent encoding
- **THEN** configuration loading MUST reject it with an error identifying `instance.browser_base` before exposing inconsistent browser or administration routes
- **AND** supported root and unescaped subpath bases MUST remain accepted

### Requirement: Browser authentication routes remain aligned with the deployment base

A root-mounted Marketplace deployment that enables browser administration SHALL configure the Marketplace browser base at that root. Supplied deployment defaults and operator guidance SHALL keep the web-interface base, administration destinations, sign-in error routes, and Marketplace return route aligned while preserving the absent-base legacy fallback for configurations that do not select the root-mounted profile.

#### Scenario: Root-mounted administration uses root browser routes

- **WHEN** a Marketplace is deployed with its web interface and configured browser base at the origin root
- **THEN** login and callback destinations MUST accept `/admin` and descendants and reject legacy `/marketplace/admin` destinations
- **AND** browser failures and missing destinations MUST use `/auth/error` and `/admin` respectively

#### Scenario: Subpath and absent browser bases retain their route contracts

- **WHEN** administration uses an explicit subpath browser base or no browser base
- **THEN** login, callback, error, return, and fallback routes MUST use the configured subpath or the documented legacy `/marketplace` paths respectively

### Requirement: Administration session stores only admin-relevant groups

After a successful OIDC administration callback, the signed session cookie SHALL record administration group membership as a bitset over the configured `admin_groups` list, where a bit is set only for each configured group that is among the verified ID-token groups. The session SHALL also record a fingerprint of the configured `admin_groups` list, and SHALL NOT store group names. When access is granted only through `admin_users`, the bitset SHALL be empty. The session SHALL store the verified subject, issuer, display name, and lifetime as before. A session whose fingerprint does not match the currently configured `admin_groups` list SHALL be treated as unauthenticated. Per-request administration authorization SHALL otherwise be unchanged: a session whose matched groups intersect `admin_groups` SHALL be admitted, a session whose subject is listed in `admin_users` SHALL be admitted, and a session with neither SHALL receive `403 forbidden`. Session cookie size SHALL NOT depend on the number of groups the identity provider returns for the user or on the length of their group names.

#### Scenario: Only matching groups are stored in the session

- **WHEN** a verified ID token with 256 groups, exactly one of which is listed in `admin_groups`, completes the administration callback
- **THEN** the session records exactly that one configured group in its membership bitset, and the session is admitted for administration requests

#### Scenario: Access through admin_users stores no groups

- **WHEN** a verified identity whose subject is listed in `admin_users` and which holds no `admin_groups` membership completes the callback
- **THEN** the session is issued with an empty membership bitset and is admitted for administration requests

#### Scenario: Callback for a large group list stays within the proxy header budget

- **WHEN** a verified ID token with 256 groups, one of which is in `admin_groups`, is authorized through `admin_groups` and the callback responds with `303`
- **THEN** the serialized callback response header block is under 4096 bytes

#### Scenario: Maximal identity values stay within the proxy header budget

- **WHEN** a verified identity with a 512-byte subject, a 512-byte display name, and matching `admin_groups` entries of 512 bytes each completes the callback with a sign-in destination of 1024 bytes
- **THEN** the serialized callback response header block is under 4096 bytes and the callback responds with `303`

#### Scenario: Identity with neither membership is forbidden after narrowing

- **WHEN** a verified identity whose subject is not in `admin_users` and whose groups do not intersect `admin_groups` completes the callback
- **THEN** the callback responds `403 forbidden` and issues no session cookie

#### Scenario: Stored membership is rejected after admin_groups changes

- **WHEN** a session sealed for one `admin_groups` list is presented after the operator changes `admin_groups`
- **THEN** the request is rejected as unauthenticated and the operator must sign in again

### Requirement: Session cookie overflow is logged without identity values

When the administration callback cannot seal the session cookie within its size bound, the callback SHALL log the outcome `session_too_large` with no subject or group values, SHALL set no session cookie, and SHALL respond with the existing `401 authentication_rejected` response.

#### Scenario: Oversized session is rejected with a size outcome

- **WHEN** the administration callback authorizes an identity but the configured `admin_groups` list is too large for its membership record to be sealed within the cookie bound
- **THEN** the log records `outcome=session_too_large` without subject or group values, no session cookie is set, and the response is `401 authentication_rejected`

### Requirement: Sign-in destination is bounded for the callback response

The sign-in destination SHALL consist of printable ASCII characters and SHALL be at most 1024 bytes. Login requests whose destination violates this bound SHALL be rejected before the identity provider is contacted, so that the callback `Location` header stays within the proxy header budget.

#### Scenario: Oversized or non-ASCII sign-in destination is rejected

- **WHEN** a login request names a sign-in destination longer than 1024 bytes or containing non-ASCII bytes
- **THEN** the login responds `400 authentication_request_invalid` and no login transaction cookie is set

### Requirement: OIDC issuer length is bounded

The configured OIDC issuer SHALL be at most 512 bytes. Configuration containing a longer issuer SHALL be refused during configuration loading, so that the verified issuer recorded in the administration session has a fixed upper bound.

#### Scenario: Issuer longer than 512 bytes is refused at configuration

- **WHEN** the administration OIDC configuration names an issuer longer than 512 bytes
- **THEN** configuration loading fails and the administration surface is not enabled
