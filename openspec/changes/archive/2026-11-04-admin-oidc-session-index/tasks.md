## 1. Session membership index

- [x] 1.1 Red: callback cases asserting the session stores a bitset and fingerprint and no group names, and that a 256-group token with one match is admitted (`only-matching-groups-are-stored-in-the-session`).
- [x] 1.2 Red: session sealed under a different `admin_groups` list is rejected (`stored-membership-is-rejected-after-admin-groups-changes`).
- [x] 1.3 Implement the bitset payload (version 2), fingerprint, and session decoding in `pkg/marketplace/api/admin_oidc.go`. Verified: admin tests pass.

## 2. Header budget

- [x] 2.1 Callback case with maximal identity values and a 1024-byte sign-in destination stays under 4096 bytes (`maximal-identity-values-stay-within-the-proxy-header-budget`). Measured: sealed session 1642 bytes, header block 3082 bytes.
- [x] 2.2 Sign-in destination bounded to printable ASCII and 1024 bytes (`oversized-or-non-ascii-sign-in-destination-is-rejected`).

## 3. Proxy gate case

- [x] 3.1 Callback proxy case uses a stub `Set-Cookie` at the measured maximal sealed size, asserting `303` under the default buffer. Verified: `make marketplace-proxy-check` passes in Docker.

## 4. Documentation and release note

- [x] 4.1 Reverse-proxy docs state the 4096-byte budget and the bitset session.
- [x] 4.2 Release note matches the implemented behavior.

## 5. Specs

- [x] 5.1 Scenario tests annotated; `go run ./tools/specaudit trace` reports no unbound scenarios.
- [ ] 5.2 Archive the change and pass `make spec-check`.
