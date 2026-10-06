## MODIFIED Requirements

### Requirement: Browser-aware OIDC failure responses

OIDC login and callback failures SHALL issue a 303 redirect to a same-origin sign-in error page beneath the path component of the validated configured Marketplace browser base only when the request explicitly prefers an acceptable `text/html` response over JSON. A root browser base SHALL resolve that page to `/auth/error`; an explicit non-root base SHALL prefix the same route with its path. When no browser base is configured, the existing `/marketplace/auth/error` fallback SHALL remain. Missing `Accept`, wildcard-only `Accept`, JSON preference, and tied preference SHALL retain existing JSON error bodies and status codes. This policy SHALL cover request-origin and OIDC initialization failures at these endpoints without changing logout or administration data responses.

#### Scenario: Browser and machine clients receive appropriate failure responses

- **WHEN** OIDC login or callback fails due to provider denial, invalid or expired transaction, invalid state or token, malformed response, provider outage, rejected origin, or unavailable authentication configuration
- **THEN** HTML-preferring requests receive a 303 same-origin error-page redirect beneath the configured Marketplace browser-base path
- **AND** root, explicit non-root, and absent browser-base configurations resolve respectively to the root route, prefixed route, and legacy fallback
- **AND** JSON-preferring, missing-`Accept`, and wildcard-only requests retain the corresponding existing JSON error code and HTTP status

### Requirement: Explicit safe sign-in recovery

The error page SHALL provide a return link to the configured Marketplace browser base and an explicit fresh-sign-in action for retryable failures. Retry destinations SHALL be limited to validated same-origin administration paths beneath that browser base and otherwise fall back to its administration root. When no browser base is configured, existing `/marketplace` return and administration fallback paths SHALL remain. Authorization or configuration guidance SHALL NOT imply retry grants permission, and no sign-in or administration operation SHALL be retried automatically.

#### Scenario: Recovery starts a new login with a safe destination

- **WHEN** a user follows the recovery action after a retryable failure with a valid administration destination, or with an unsafe or missing destination
- **THEN** a new login transaction starts with the validated destination or the administration-root fallback beneath the configured Marketplace browser-base path
- **AND** root, explicit non-root, and absent browser-base configurations use their corresponding root, prefixed, or legacy return and fallback paths
- **AND** every failure page offers the applicable Marketplace return link without automatically replaying an operation
