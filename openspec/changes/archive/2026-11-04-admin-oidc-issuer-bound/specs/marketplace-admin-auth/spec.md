## ADDED Requirements

### Requirement: OIDC issuer length is bounded

The configured OIDC issuer SHALL be at most 512 bytes. Configuration containing a longer issuer SHALL be refused during configuration loading, so that the verified issuer recorded in the administration session has a fixed upper bound.

#### Scenario: Issuer longer than 512 bytes is refused at configuration

- **WHEN** the administration OIDC configuration names an issuer longer than 512 bytes
- **THEN** configuration loading fails and the administration surface is not enabled

## MODIFIED Requirements

### Requirement: Session cookie overflow is logged without identity values

When the administration callback cannot seal the session cookie within its size bound, the callback SHALL log the outcome `session_too_large` with no subject or group values, SHALL set no session cookie, and SHALL respond with the existing `401 authentication_rejected` response.

#### Scenario: Oversized session is rejected with a size outcome

- **WHEN** the administration callback authorizes an identity but the configured `admin_groups` list is too large for its membership record to be sealed within the cookie bound
- **THEN** the log records `outcome=session_too_large` without subject or group values, no session cookie is set, and the response is `401 authentication_rejected`
