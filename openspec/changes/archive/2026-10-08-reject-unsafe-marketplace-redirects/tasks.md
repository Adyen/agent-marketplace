## 1. Regression

- [x] 1.1 Execute every supplied Nginx profile with unsafe legacy paths and valid controls; bind both scenarios to the executable test.
- [x] 1.2 Observe the redirect regression fail against the original profiles.

## 2. Fix and verification

- [x] 2.1 Reject decoded backslashes and ASCII controls before legacy redirects in all three profiles.
- [x] 2.2 Require the executable regression in deployment CI and verify its selection and witness checks.
- [x] 2.3 Run focused deployment tests, required validation, and an adversarial pre-push review.
- [x] 2.4 Reproduce unsupported encoded browser bases and reject them at configuration loading without relaxing destination safeguards.
- [x] 2.5 Correct root-profile migration guidance and validate supported frontend and authentication paths.

## 3. Archive

- [x] 3.1 Run `openspec archive reject-unsafe-marketplace-redirects --yes` as the final commit of the change.
