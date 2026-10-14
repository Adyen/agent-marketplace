## Why

Legacy Marketplace compatibility redirects copy a decoded backslash into a
`Location` header. Browsers interpret the resulting `/\host` destination as a
different origin, violating the documented same-origin routing contract.
Decoded tabs and line breaks can similarly expose an authority or corrupt
the redirect header.

## What Changes

- Refuse legacy browser paths containing decoded backslashes or ASCII controls with HTTP 400
  and no redirect.
- Preserve ordinary root-profile migration redirects and query strings.
- Reject unsupported encoded browser bases during configuration loading.
- Align reverse-proxy migration guidance with the root-mounted defaults.
- Execute all three supplied Nginx profiles in a required deployment check,
  with attack cases and valid routing controls.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `site-deployment-profiles`: unsafe legacy redirect candidates fail closed.
- `marketplace-admin-auth`: unsupported encoded browser bases fail during configuration loading.

## Impact

The SQLite, PostgreSQL, and local-operator proxy profiles, deployment
validation, the CI deployment check, browser-base configuration validation,
and operator migration guidance. OIDC and frontend destination safeguards
remain unchanged.
