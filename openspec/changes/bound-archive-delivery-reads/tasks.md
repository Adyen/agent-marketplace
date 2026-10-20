## Implementation

- [x] Add failing regressions for the artifact and placeholder archive routes, with and without a stored placeholder, within and above the artifact limit, plus the immutable blob comparison.
- [x] Read archive blobs through the bounded capability at the artifact limit and map limit refusals to the existing `413` response.
- [x] Bound the immutable blob comparison by the upload size.
- [x] Prove the bounds with negative mutations; run Marketplace API, store and E2E consumers.
- [x] Verify scoped tests, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [ ] `openspec archive bound-archive-delivery-reads --yes`
