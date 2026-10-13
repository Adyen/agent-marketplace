## Implementation

- [x] Add failing redirect-policy and userinfo-with-port diagnostic regressions with default-redirect controls.
- [x] Copy `http.followRedirects` from trusted configuration, accepting a bare key as `true`.
- [x] Canonicalize the credential-free URL for transport classification and diagnostics.
- [x] Document Git redirect semantics and the copied setting in the private registry guide and environment reference.
- [x] Verify scoped tests, lint, build, strict specs and the combined E2E gate.
- [x] Adversarially review every changed entry.
