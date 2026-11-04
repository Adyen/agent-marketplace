## ADDED Requirements

### Requirement: Administration session stores only admin-relevant groups

After a successful OIDC administration callback, the signed session cookie SHALL store only the verified ID-token groups that are members of the configured `admin_groups`. When access is granted only through `admin_users`, the session SHALL store no groups. The session SHALL store the verified subject, issuer, display name, and lifetime as before. Per-request administration authorization SHALL be unchanged: a session whose stored groups intersect `admin_groups` SHALL be admitted, a session whose subject is listed in `admin_users` SHALL be admitted, and a session with neither SHALL receive `403 forbidden`. Session cookie size SHALL NOT depend on the number of groups the identity provider returns for the user.

#### Scenario: Only matching groups are stored in the session

- **WHEN** a verified ID token with 256 groups, exactly one of which is listed in `admin_groups`, completes the administration callback
- **THEN** the decoded session payload contains exactly that one group, and the session is admitted for administration requests

#### Scenario: Access through admin_users stores no groups

- **WHEN** a verified identity whose subject is listed in `admin_users` and which holds no `admin_groups` membership completes the callback
- **THEN** the session is issued with no stored groups and is admitted for administration requests

#### Scenario: Callback for a large group list stays within the proxy header budget

- **WHEN** a verified ID token with 256 groups, one of which is in `admin_groups`, is authorized through `admin_groups` and the callback responds with `303`
- **THEN** the serialized callback response header block is under 4096 bytes

#### Scenario: Identity with neither membership is forbidden after narrowing

- **WHEN** a verified identity whose subject is not in `admin_users` and whose groups do not intersect `admin_groups` completes the callback
- **THEN** the callback responds `403 forbidden` and issues no session cookie

### Requirement: Session cookie overflow is logged without identity values

When the administration callback cannot seal the session cookie within its size bound, the callback SHALL log the outcome `session_too_large` with no subject or group values, SHALL set no session cookie, and SHALL respond with the existing `401 authentication_rejected` response.

#### Scenario: Oversized session is rejected with a size outcome

- **WHEN** the administration callback authorizes an identity whose matching `admin_groups` intersection cannot be sealed within the cookie bound
- **THEN** the log records `outcome=session_too_large` without subject or group values, no session cookie is set, and the response is `401 authentication_rejected`
