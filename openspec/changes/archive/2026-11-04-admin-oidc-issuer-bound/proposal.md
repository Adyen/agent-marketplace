## Why

The administration session stores the verified issuer as `iss` in the sealed cookie, but configuration validation does not bound the issuer length. A very long issuer can push the callback response header block past the 4096-byte proxy buffer, which the session-index change otherwise guarantees. Review of PR #1003 flagged the gap.

## What Changes

- Configuration validation refuses an OIDC issuer longer than 512 bytes, so the sealed session's issuer field has a fixed upper bound.
- The `session_too_large` requirement is updated to describe the current failure case: the configured `admin_groups` list cannot be sealed, not a matching intersection.

**Not changed:** issuer scheme, path, and query rules, the session payload, and the callback response format.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-admin-auth`: adds the issuer length bound and corrects the session overflow scenario.

## Impact

- `pkg/marketplace/config/administration.go`: issuer length check in `validateOIDC`.
- `pkg/marketplace/config/administration_test.go`: over-length issuer case.
- Operators with an issuer longer than 512 bytes see a configuration error at startup and must shorten the issuer, which no supported identity provider requires.

## Release note

Administration OIDC issuer URLs longer than 512 bytes are now refused at configuration load. Issuers of ordinary length are unaffected.
