## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Sign-in destination is bounded for the callback response

The sign-in destination SHALL consist of printable ASCII characters and SHALL be at most 1024 bytes. Login requests whose destination violates this bound SHALL be rejected before the identity provider is contacted, so that the callback `Location` header stays within the proxy header budget.

#### Scenario: Oversized or non-ASCII sign-in destination is rejected

- **WHEN** a login request names a sign-in destination longer than 1024 bytes or containing non-ASCII bytes
- **THEN** the login responds `400 authentication_request_invalid` and no login transaction cookie is set
