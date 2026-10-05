## Context

See proposal.md for the motivation. The Go administration router exposes OIDC login and callback as GET routes. Their failures currently use the JSON administration error writer; the shared request protections also write JSON. The login transaction already carries a validated administration destination, is consumed before callback exchange, and is followed by provider verification and authorization before a session cookie is issued. The Nuxt administration catch-all is client-rendered and loads administration session and configuration data, while the security middleware applies a site-wide Content Security Policy and a less restrictive referrer policy. The sign-in error view must remain independent of that authenticated page flow.

## Goals / Non-Goals

**Goals:**
- Negotiate a browser-only error handoff for OIDC login and callback while preserving the existing JSON error response contract.
- Render safe sign-in explanations and explicit recovery independently of administration session and configuration requests.
- Preserve authentication, authorization, transaction, cookie, and provider-verification behavior while applying private response headers.

**Non-Goals:**
- Changing authorization policy, provider or token verification, logout behavior, or administration data responses.
- Automatically retrying sign-in or replaying an administration operation.
- Redesigning the administration dashboard or global typography, or adding dependencies.

## Decisions

1. **Negotiate by `Accept`, not user-agent.** Use an explicit, acceptable `text/html` preference only when its positive quality is stronger than JSON; ties remain JSON. Honor exclusions, and fall back to JSON for missing, wildcard-only, or malformed `Accept` values. This makes the browser handoff a declared representation choice while retaining the current machine-client path.

2. **Keep the handoff boundary narrow.** Apply a shared browser-aware responder only to exact GET OIDC login and callback requests. Cover their OIDC failures, request-protection failures, and OIDC initialization failures; leave logout, other administration routes, and administration data responses on their existing JSON behavior. JSON continues through the existing error writer. The browser response is a 303 to the fixed same-origin `/marketplace/auth/error` route with an allowlisted code and only an available validated destination; it does not expose the original diagnostic text.

3. **Use a dedicated server-rendered route outside the administration catch-all.** Render fixed explanations for supported codes and a generic fallback for unknown, duplicated, or malformed parameters. Do not fetch administration session or configuration data while rendering. Keep copy derived only from the error code, not query-provided descriptions or provider text. Reuse the existing login-link destination validation for the explicit fresh-login action; use the administration root when a destination is unsafe or unavailable. Forbidden, configuration, and origin outcomes give operator guidance rather than implying that retry grants permission.

4. **Preserve OIDC security invariants.** Keep provider verification, authorization checks, signed transaction validation and consumption, and session-cookie issuance unchanged. Do not forward provider descriptions, state, authorization codes, tokens, or identity claims in the redirect or page. Provider `access_denied` remains the generic `authentication_rejected` outcome; `forbidden` remains reserved for a verified identity that fails administration authorization. Recovery starts a new login transaction and never automatically replays a prior operation.

5. **Make the handoff and page private without weakening the site policy.** Set `Cache-Control: no-store`, noindex robot controls, and `Referrer-Policy: no-referrer` on both the handoff and page, while retaining the existing Content Security Policy. Add route-specific rules for the error view rather than inheriting the administration catch-all's client-only behavior. Follow the Marketplace visual conventions with a restrained status rail, generous left-aligned spacing, a local serif headline, existing tokens and icons, safe mobile wrapping, visible keyboard focus, and no mandatory motion; support light/dark appearance and reduced-motion preferences.

6. **Keep published contracts aligned.** Update the canonical OpenAPI route descriptions to document browser redirects alongside unchanged JSON errors, regenerate client types from that API, and describe safe recovery in the operator administration guide.

## Risks / Trade-offs

- A browser 303 does not preserve the originating HTTP failure status for the navigated document; JSON clients retain the existing statuses and bodies.
- Error parameters and destinations are untrusted input; fixed code-based copy, duplicate/malformed-value fallback, and destination validation prevent them from becoming diagnostics or open redirects.
- The existing administration route loads session and configuration data; placing the error view under its catch-all could accidentally trigger those requests. Keep it a separate route and verify the no-session/no-configuration path in browser coverage.
- The global security middleware currently uses a less restrictive referrer policy; the error route and handoff must explicitly apply no-referrer without removing the existing CSP.

## Migration Plan

No persisted data or configuration migration is required. Deploy the API, frontend, OpenAPI/generated types, and operator-guide updates together. Rolling back the application restores the prior browser JSON failure behavior without changing authentication configuration or session-cookie format.
