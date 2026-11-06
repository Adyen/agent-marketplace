## Context

The session-index change bounds the sealed session's membership bitset, the fingerprint, the subject, the display name, and the sign-in destination. The issuer is also written into the sealed session as `iss`. Without a bound, a long issuer defeats the 4096-byte header budget that the proxy gate and the header-budget test assume.

## Decisions

- **512-byte issuer cap in configuration.** It matches the existing bound for identity values and admin list values. The check runs in `validateOIDC`, alongside the other issuer rules, so it fails at configuration load and never at login.
- **Byte length, not parsed length.** The cap uses `len(o.Issuer)`, so it bounds what is actually written into the cookie.
- **Corrected overflow scenario.** The `session_too_large` scenario now describes the membership-record case that the code actually produces, so the spec no longer describes a matching-intersection failure that no longer exists.

## Risks

- An operator with an issuer over 512 bytes gets a startup error. Identity providers use issuer URLs far below this length.
