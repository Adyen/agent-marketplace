## 1. Regression coverage

- [ ] 1.1 Add failing pipeline coverage with a fake Marketplace: one pin 404, one updatable pin; all pins 404; several pins 404; a collection pin 404; a registry 404 and a server error that still abort without mutation.
- [ ] 1.2 Add failing command coverage proving the report, exit error with the remove-and-install hint, byte-identical missing pin lock entry, and a clean `sync --check`.

## 2. Implementation

- [ ] 2.1 Classify a 404 from the latest-revision lookup as a typed missing-resource error in `internal/marketplace`.
- [ ] 2.2 Record missing pins as skipped plans in Marketplace update planning and return the update result with an error naming every missing pin.
- [ ] 2.3 Write the update report before returning the missing-resource error from `update`.

## 3. Verification

- [ ] 3.1 Run focused pipeline, marketplace, and command tests, `make spec-check`, and `make validate`.
