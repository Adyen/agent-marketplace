## 1. Regression contract

- [x] 1.1 Add failing workflow tests under tools/ci-plan for public Cloud destination, supported triggers, same-repository and Dependabot eligibility, main-only dispatch, and least-privilege secret scope. Annotate eligible-revisions-use-the-public-service and untrusted-requests-receive-no-analysis-credentials scenarios under regression-gates.
- [x] 1.2 Add an executed eligibility-shell test using empty and dummy nonempty tokens; assert skip output, enabled output, and absence of token contents. Annotate missing-credentials-produce-an-explicit-skip.
- [x] 1.3 Add matching-report provenance and failure tests: credential-free coverage command arguments, failed go test propagation, required report upload, same-run artifact identity, scanner dependency and no continue-on-error. Annotate analysis-imports-fresh-matching-coverage and analysis-failures-remain-visible. Record expected red failures before implementation.

## 2. Cloud integration

- [x] 2.1 Add the standalone GitHub-hosted workflow with eligibility, root Go short-mode coverage, and isolated scanner jobs as specified in design.md. Keep the existing CI workflows and branch-protection requirements unchanged.
- [x] 2.2 Add sonar-project.properties for the public adyen organization and proposed Adyen_agent-marketplace project key, source/test classification, fixture/generated/dependency exclusions, and Go coverage import. Do not configure unmeasured frontend coverage exclusions.
- [x] 2.3 Verify the affected tools/ci-plan tests pass. Review the complete diff for secret exposure, untrusted triggers, stale-report reuse, unintended writes, and failure masking.

## 3. Verification and delivery

- [x] 3.1 Run focused tools/ci-plan tests and validate the GitHub workflow syntax with actionlint if available; record any unavailable external tooling rather than claiming it ran.
- [x] 3.2 Run make validate once with TEST_PACKAGES=./tools/ci-plan and the repository spec gate. Leave pre-existing go.work.sum and any untracked token file untouched and out of commits.
- [ ] 3.3 Record Cloud project/token onboarding steps, fork skips, partial Go-only coverage, and the unverified live scan in the PR description. Commit intended files only, perform the pre-push adversarial review, push the branch, and open the PR against main. Do not create secrets or change protection settings.
- [ ] 3.4 openspec archive add-sonarqube-cloud-ci --yes
