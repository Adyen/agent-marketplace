## 1. Tests

- [x] 1.1 Add the Go real-handler HTML/JSON failure regression matrix for `browser-and-machine-clients-receive-appropriate-failure-responses` and `failed-authentication-does-not-leak-diagnostics-or-issue-a-session`; annotate both scenarios and observe the intended red against the base behavior while retaining JSON/status/cookie assertions.
- [x] 1.2 Add frontend presentation, browser-recovery, privacy, accessibility, and published-contract regressions for `failure-page-renders-without-a-session`, `recovery-starts-a-new-login-with-a-safe-destination`, `error-presentation-is-private-and-accessible`, and `published-contract-matches-browser-and-json-behavior`; annotate all four scenarios and observe the intended red against the base behavior.

## 2. Implementation

- [x] 2.1 Implement the shared browser-aware OIDC error handoff with initialization and request-protection coverage; verify the focused Go failure and Accept-negotiation matrix passes while JSON responses remain unchanged.
- [x] 2.2 Implement the dedicated server-rendered error page, fixed safe copy, validated fresh-login destination, private headers, and accessible responsive presentation; verify frontend tests and browser journeys prove no administration fetch, no-JavaScript rendering, safe recovery, and privacy/accessibility behavior.
- [x] 2.3 Update canonical OpenAPI route descriptions, generated client types, and the operator administration guide; verify generated API types are in sync and published behavior matches the JSON and browser contracts.

## 3. Verification

- [x] 3.1 Run focused Go and frontend tests and capture desktop/mobile light/dark visual evidence outside the worktree; verify the failure, recovery, header, and accessibility journeys pass.
- [ ] 3.2 Run `make validate` once, `make spec-check`, `openspec validate --all --strict`, and the contract's frontend lint, typecheck, API-drift, unit, Nuxt-build, and focused Playwright checks; verify every required check passes.

## 4. Delivery

- [ ] 4.1 Reconcile the operator documentation and published API contract with the implemented behavior; verify browser redirects, JSON compatibility, and safe recovery are described consistently.
- [ ] 4.2 Complete independent acceptance and adversarial review and obtain green PR CI; verify review findings are resolved or recorded and required CI is successful.
- [ ] 4.3 Run `openspec archive oidc-browser-error-pages --yes` as the final task in the implementation PR; verify the archived capability spec contains the six reviewed scenarios.
