## 1. Consumer integration

- [x] Verify the external-config edit regression fails on main using a valid inert v4 configuration.
- [x] Port config/lock snapshots and publication without bypassing the current LayerIntent/LayerEditor APIs.
- [x] Preserve approved user and contained repository/local links, logical lock ownership and current missing-root/dangling behavior.
- [x] Verify scope/alias refusals, descriptor replacement barriers, transaction recovery and no-op metadata.

## 2. Verification and delivery

- [x] Review every changed source, test and spec with open-code-review-delegate; correct actionable findings.
- [x] Run affected package, command, repeated race and benchmark checks.
- [x] Run make validate and make spec-check; retain the combined E2E dangling-user-config failure as a fixed regression.
- [x] Archive the verified change; leave dependent delivery outside the archive task list.
- [ ] Push the reviewed stack and update #431/#215; leave unimplemented R2 consumers open.
