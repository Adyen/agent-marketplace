## 1. Scheme and request handling

- [ ] 1.1 Register the `agent-marketplace` URL scheme in the macOS app bundle and Linux desktop entry; add annotated platform tests and verify both launch requests reach the same handler.
- [ ] 1.2 Implement bounded parsing of the exact install-link format; add annotated Go tests for exact digest, omitted digest, unknown/repeated/malformed/oversized parameters, visible rejection reasons, and no side effects.
- [ ] 1.3 Implement cold-launch, running-instance handoff, first-run continuation, and busy rejection; add annotated Playwright tests verifying pending requests survive first run and busy links are rejected and discarded.

## 2. Resolution and confirmation

- [ ] 2.1 Resolve exact/latest Resource revisions and full plans through shared dependency reconciliation; add annotated tests for Marketplace verification, dependencies, collection members, and plan freshness.
- [ ] 2.2 Build the explicit confirmation with Marketplace, Resource coordinate/revision, user scope, Harness targets, complete plan, and hook/MCP command warning; add annotated tests proving no write occurs before confirmation and cancellation writes nothing.
- [ ] 2.3 Enforce effective `allowed_marketplaces`, verified identity for unregistered Marketplaces, and Marketplace-equivalent concise refusal reasons for Revoked revisions and security-policy blocks; add annotated allow/deny, registration, decline, and policy-refusal tests.

## 3. Hosted UI and finalization

- [ ] 3.1 Add “Open in app” beside command copy for installable resources and retain the existing copy behavior; add annotated Marketplace UI tests proving both actions are always shown regardless of app detection and the link carries the displayed Marketplace endpoint, Resource coordinate, and revision digest.
- [ ] 3.2 Keep “Open in app” absent from unavailable resources and preserve the concise unavailable notice; add annotated tests for policy-blocked and other unavailable reasons.
- [ ] 3.3 Run Go link-handler tests, Playwright confirmation flows, Marketplace UI tests, `make spec-check`, and affected validation; require zero trace findings for this layer, with no unbound scenarios or dangling annotations.
- [ ] 3.4 Run `openspec archive gui-install-deep-link --yes` in this layer's pull request.
