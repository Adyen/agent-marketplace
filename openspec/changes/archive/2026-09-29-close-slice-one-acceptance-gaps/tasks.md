## 1. Configuration and cache cleanup

- [x] 1.1 Write and run the duplicate-alias test with the `marketplace-protocols/same-endpoint-alias-with-different-instance-ids` witness; verify it fails on the base and passes after removing the provisional-instance exception.
- [x] 1.2 Centralize missing Marketplace evidence detection and remove the unused configuration loader facade; verify focused config, pipeline, and command bootstrap tests pass and production caller search has no facade.
- [x] 1.3 Remove nil-context fallbacks in cache operations and repair actual nil callers; verify focused cache tests, cancellation checks, and `make ci-lint` pass.

## 2. Cache collection evidence

- [x] 2.1 Factor the organization Marketplace GC command fixture and add the empty-cache case tagged `cache-garbage-collection/organization-marketplace-with-empty-cache`; verify zero removals, requests, and organization writes.
- [x] 2.2 Add the trusted organization acquisition lock case tagged `cache-garbage-collection/trusted-organization-lock-retains-exact-archive`; verify retention, real orphan deletion, unchanged lock bytes, and zero network requests.

## 3. As-built evidence

- [x] 3.1 Correct the design, plan, and development guidance to describe the stack and the deferred decoder/TUI seams; verify each claim against source and branch history.
- [ ] 3.2 Capture ten benchmark samples at the specified stack baseline and the layer head, run benchstat for the named matched cases, and report the unpaired Marketplace GC case and endpoint-walk I/O in the PR body.
- [ ] 3.3 Run focused and native build, test, lint, and strict OpenSpec/specaudit checks and record the section-4/section-7 evidence on the parent task.
- [x] 3.4 Run `openspec archive close-slice-one-acceptance-gaps --yes` in this pull request and verify the two capabilities are synced into the main specs.
