## 1. Consumer integration

- [x] Verify the external-config edit regression fails on main using a valid inert v4 configuration.
- [ ] Port config/lock snapshots and publication without bypassing the current LayerIntent/LayerEditor APIs.
- [ ] Preserve approved user and contained repository/local links, logical lock ownership and current missing-root/dangling behavior.
- [ ] Verify scope/alias refusals, descriptor replacement barriers, transaction recovery and no-op metadata.

## 2. Verification and delivery

- [ ] Review every changed source, test and spec with open-code-review-delegate; correct actionable findings.
- [ ] Run affected package, command, repeated race and before/after benchmark checks.
- [ ] Run make validate, make spec-check and the combined E2E/configuration gate.
- [ ] Archive with openspec archive anchor-scoped-config-publication --yes as the final implementation commit.
- [ ] Push the reviewed stack and update #431/#215; leave unimplemented R2 consumers open.
