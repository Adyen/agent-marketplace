# Tasks

## 1. Prove one stock CLI Job before building the scheduler

- [ ] 1.1 Extract or reuse ingestion's bounded skill-input selection. Test an index-only registry fixture without publisher CI: include the authored primary document and selected support files, exclude siblings and Git metadata, and refuse links or out-of-budget input.
- [ ] 1.2 Add a disposable Kubernetes witness with a pinned unmodified CLI image, per-attempt input/output volumes and an enforcing network policy. Document its prerequisites and one command to run it. Prove denied DNS and TCP egress against a reachable positive control, no credentials, read-only input and a non-root process. Bind `marketplace-delivery-security/stock-cli-returns-a-private-index-only-skill-report`.
- [ ] 1.3 Read native SkillSpector JSON, not the runner response, and feed only validated evidence through the existing assessment path. Drive a real scan of the fixture: the resource API shows the correct content digest, partial coverage and `SC4` on `support/requirements.txt`; its built-in decision stays unchanged and the index-only archive remains unavailable. Bind `marketplace-analysis/third-party-registry-supplies-no-scan-report`.
- [ ] 1.4 Test CLI exits 0 and 1 with valid reports, exit 2, timeout, invalid version, oversized reports and unsafe output paths. Verify bounded failure and cleanup, including interrupted collection. Bind `marketplace-delivery-security/untrusted-output-or-interrupted-attempt-is-contained`. Record image digest, elapsed time, cleanup and isolation results without private content. Stop here if the real Job witness fails or skips.

## 2. Account for all indexed skills

- [ ] 2.1 Add revision/analyzer work records and public-safe pending diagnostics with SQLite/PostgreSQL conformance tests. Index multiple skills in both acquisition modes, restart between publication and dispatch, and prove every enabled pair remains accounted for and publication does not wait. Bind `marketplace-analysis/outstanding-work-survives-the-registry-deadline`.
- [ ] 2.2 Implement fair, concurrency-limited claims, expiring leases, bounded retries and orphan cleanup. Simulate duplicate workers and a crash on each side of completion. Verify idempotent outcomes, no volume reuse between attempts and an explicit failure when exact input cannot be reacquired.
- [ ] 2.3 Finalize and attach normalized evidence transactionally. Race two independent analyzer results, supersede the revision mid-scan and fetch old assessment digests; assert no lost report, changed policy decision or stale evidence. Bind `marketplace-analysis/late-result-cannot-qualify-a-newer-revision`.
- [ ] 2.4 Reconcile operator-selected analyzers for existing heads, including configuration changes, removals and repeat refreshes. Use two test profiles and prove unchanged work is reused and only the changed profile reruns. Bind `marketplace-analysis/changing-one-analyzer-does-not-invalidate-another`.

## 3. Offer the Kubernetes mode without changing defaults

- [ ] 3.1 Wire the dispatcher to operator profiles with immutable image pins and known adapters. Render and exercise the limited namespace permissions, storage quotas, isolation and admission requirements. A fixture containing registry-authored job settings must not affect the Job. Bind `marketplace-delivery-security/registry-content-cannot-configure-execution`.
- [ ] 3.2 Document activation, image provenance, troubleshooting backlog/cleanup and rollback in the Helm guide. Verify legacy runner and Compose defaults, opt-in Job mode, refusal of conflicting modes and no analyzer dependencies or new Kubernetes permissions in the worker.
- [ ] 3.3 Repeat the real third-party witness through automatic dispatch, without the milestone-one harness scheduling the Job. Check durable retry, index-only and mirror coverage, stable assessment lookup, zero retained scan volumes and no public executable artifact for index-only skills. Run strict spec validation and scenario tracing.

## Workflow follow-up

- After the implementation and its tests pass, run `openspec archive run-isolated-analyzer-jobs --yes` in the implementation pull request.
