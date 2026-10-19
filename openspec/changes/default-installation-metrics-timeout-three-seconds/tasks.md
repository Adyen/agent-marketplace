## 1. Default timeout

- [ ] 1.1 Update the timing-defaults regression for `marketplace-api/existing-configuration-uses-defaults-independently-of-reindexing` to expect a three-second timeout and verify it fails before the change.
- [ ] 1.2 Set `defaultInstallationMetricsTimeout` to three seconds and verify `CGO_ENABLED=0 go test ./pkg/marketplace/config -count=1` passes, including the existing override and validation cases.

## 2. Completion

- [ ] 2.1 Update the operator configuration and HTTP API reference pages to state the three-second default and run `openspec archive default-installation-metrics-timeout-three-seconds --yes`; verify `make spec-check` passes.
