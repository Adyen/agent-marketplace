## Context

See proposal.md - Why. `handleCallback` builds `adminSessionPayload.Groups` from every ID-token group that passed `claimGroups`, which accepts up to `maxAdminGroups` (256). `seal()` rejects any sealed value above `maxAdminCookie` (3900 bytes). The session's only consumers are `sessionIdentity` (per-request identity) and `adminAuthorized` (admin_users subject or admin_groups membership).

## Goals / Non-Goals

**Goals:** store only `groups ∩ admin_groups` in the session; keep per-request authorization decisions identical; make the size failure observable by outcome.

**Non-Goals:** changing the login transaction, PKCE, nonce, state, or ID-token validation; changing cookie name, attributes, lifetime, or payload format; encrypting the payload; changing `maxAdminCookie` or `maxAdminGroups`; changing proxy buffer defaults.

## Decisions

- **Narrow at callback, not at read.** Filter the verified group list against `admin_groups` once, when the session is built. The alternative, storing everything and filtering in `sessionIdentity`, keeps the oversized cookie and the 502 on the callback response, so it does not solve the reported failure.
- **Keep `adminAuthorized` as the single authorization check.** The callback already calls it on the full verified groups before narrowing. Per-request checks use the narrowed list, which keeps the same answer for every admitted identity, because `admin_groups` membership is the only group-based criterion.
- **Distinct outcome via error identity.** Introduce a sentinel for the cookie-bound failure returned by `seal()` and match it with `errors.Is` in the callback, so the size outcome is logged and other seal failures keep their current behavior. The log line carries no subject or group values.
- **Existing sessions keep working.** Sessions already issued with full group lists still decode and authorize through the same per-request check, so NG2 holds.

## Risks / Trade-offs

- **[Behavior change for group-only operators with group-based later checks]** → No later check reads groups beyond `admin_groups`; the existing test matrix covers admin_groups, admin_users, and forbidden cases.
- **[Oversized admin_groups still overflows the bound]** → Operator-configured admin_groups large enough to exceed the cookie bound now log `session_too_large` instead of failing silently; documented as an operator limit, not changed here.

## Migration Plan

None. Existing sessions continue to decode. Rollback is a code revert with no stored-data migration.

## Open Questions

None.
