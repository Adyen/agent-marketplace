## 1. Regression coverage

- [ ] 1.1 Add a failing command test: user configuration declares `harnesses: [factory]`, repository configuration selects a Marketplace resource, no repository lock exists; `status` still fails with `v4.marketplace-lock-required` but shows `factory` with source `USER` and no add-harnesses hint.

## 2. Implementation

- [ ] 2.1 Resolve and render the effective harness set from loaded configuration independently of acquisition-lock and Marketplace inspection failures.

## 3. Verification

- [ ] 3.1 Run focused command tests, `make spec-check`, and `make validate`.
