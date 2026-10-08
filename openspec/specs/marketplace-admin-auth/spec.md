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
