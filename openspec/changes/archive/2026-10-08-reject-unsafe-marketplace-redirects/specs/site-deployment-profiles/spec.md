## MODIFIED Requirements

### Requirement: Root-profile upgrades preserve legacy Marketplace links

The supplied root-mounted Marketplace reverse-proxy profiles SHALL redirect
safe legacy `/marketplace` browser routes to the corresponding root-mounted
web-interface routes without changing the origin or dropping the query
string. A legacy browser path containing a decoded backslash or ASCII control
character SHALL be rejected with HTTP 400 and no `Location` header. Encoded
controls and backslashes confined to the query string SHALL NOT cause that
path rejection. API and documentation
routes SHALL retain their dedicated owners.

#### Scenario: Legacy Marketplace links reach the root-mounted interface

- **WHEN** an operator deploys a supplied root-mounted Marketplace reverse-proxy profile and a user requests a safe `/marketplace` or descendant browser route
- **THEN** the proxy MUST redirect to the corresponding root-mounted route on the same origin while preserving the query string
- **AND** API and documentation requests MUST continue to reach their dedicated services

#### Scenario: Unsafe legacy browser paths never redirect

- **WHEN** a legacy Marketplace browser path contains a literal or percent-encoded backslash, including either case of its percent encoding, or a percent-encoded ASCII control character
- **THEN** every supplied root-mounted proxy profile MUST return HTTP 400 without a `Location` header
- **AND** ordinary legacy routes and query strings containing percent-encoded controls or backslashes MUST retain their normal same-origin redirect behavior
