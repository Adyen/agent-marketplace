# Tasks

## 1. Assessment and publication

- [x] 1.1 Prove historical assessment digests are unchanged, advisory evidence changes only assessment identity, and both HTTP routes return the stored evidence; add the store/API model, OpenAPI schema, and generated types. Bind the two Marketplace API identity scenarios to store and route tests.
- [x] 1.2 Prove successful mirror and index-only publication, unchanged rejection, preserved partial findings, and non-blocking failure and budget exhaustion through the worker publication path; add the injectable supplementary analyzer seam, explicitly wire the indexing roles, and bind all four Marketplace Analysis scenarios.
- [x] 1.3 Prove the advisory adapter never exports raw scanner strings or unverifiable locations; add normalization and validation and bind the Marketplace API evidence-safety scenario.

## 2. Runner and default deployments

- [x] 2.1 Add the pinned runner image and bounded request contract. Exercise the real static scanner against an inert skill and verify complete, partial, failed, and oversized input handling and no outbound network access.
- [x] 2.2 Enable the isolated runner in supplied Compose and Helm configurations and document image pinning, isolation verification, reindexing, and opt-out. Prove the default render, worker connectivity to the isolated runner, and no consumer or unconfigured-server dependency. Bind both Marketplace Delivery Security scenarios.

## 3. Resource Detail

- [x] 3.1 Add Security tab tests for independently attributed reports with partial findings, failed versus absent results, and non-skill resources. Bind the three Marketplace Web UI scenarios, keep the built-in range navigation witness green, and check desktop and mobile rendering.

## 4. Integrated Verification

- [ ] 4.1 Run scoped test-first checks for each changed boundary, then the project validation and relevant web, deployment, and Marketplace E2E gates. Require a real SkillSpector result on a published skill in a supplied deployment, not only a stub report or a runner health check.

## Workflow follow-up

- Run `openspec archive report-multiple-security-analyzers --yes` as the final commit of the implementation pull request and run `make spec-check` on the archived result.
