## Context

See proposal.md for motivation. Existing Go and web workflows use GitHub-hosted runners, immutable action pins, and job-scoped read permissions. Adyen/adyen-flutter uses sonarcloud.io, organization adyen, a repository Sonar token, and cached Android/iOS reports. Agent Marketplace has a root Go module, a separate E2E module, and Vue/TypeScript frontends; no frontend coverage reporter is installed. Its command tests already switch to serial in-process execution under Go coverage.

## Goals / Non-Goals

**Goals:** Preserve the existing CI gates and keep analysis credentials separate from execution of repository tests; make onboarding safe before the token exists; retain matching Go coverage provenance.

**Non-Goals:** Internal SonarQube access, fork analysis with elevated privileges, frontend coverage generation, changing the command-test runner, duplicating E2E gates, quality-profile customization, and making the new check required.

## Decisions

### Standalone Cloud workflow

Add .github/workflows/sonar_cloud.yml rather than coupling analysis to path-selected Go/Web jobs. Run on main pushes, pull requests targeting main, and manual dispatch limited to main. Use ubuntu-24.04 and read-only contents permissions; no pull_request_target or workflow_run. Exclude fork and Dependabot PRs from token-dependent work. Use concurrency cancellation per workflow/ref. Pin SonarSource/sonarqube-scan-action v8.3.0 to d209202bc7d53ff1cc128f7f907dac145c9d6ae9, published September 30, 2026. The older sonarcloud-github-action in Flutter is not necessary for a new integration.

### Credential-free coverage followed by isolated scanning

Use three jobs: configuration eligibility check, coverage, and analysis. The first job performs no checkout and only tests whether SONAR_TOKEN is nonempty, yielding an enabled output and a clear skip notice when absent. Coverage depends on enabled eligibility and never references SONAR_TOKEN. Run CGO_ENABLED=0 go test -short -timeout=50m -covermode=atomic -coverprofile=coverage.out ./... with the root go.mod toolchain, without changing test-runner infrastructure. Use a 60-minute coverage job budget because command coverage selects the existing serial runner. Upload only coverage.out, fail on missing output, and name the artifact with github.sha; downloading is from the same workflow run, without cache fallback or an external run id. Analysis needs successful coverage, checks out the same default workflow revision with full Git history and persist-credentials false, downloads that exact artifact, and invokes only the pinned scanner. Supply SONAR_TOKEN only to the eligibility check and scanner step, never globally. Do not execute repository install/build/test scripts in the analysis job.

### Explicit source scope and onboarding

Set sonar.host.url=https://sonarcloud.io, sonar.organization=adyen, sonar.projectKey=Adyen_agent-marketplace, UTF-8 encoding, and sonar.go.coverage.reportPaths=coverage.out in sonar-project.properties. Scope source roots to cmd,internal,pkg,tools,scripts,web,site,api,deploy,e2e. Classify Go *_test.go, frontend *.test.* and *.spec.*, web/tests, tools/tui-qa/tests, and e2e as tests, excluding the same patterns from main sources. Exclude testdata fixtures and generated frontend declarations from analysis; exclude dependency/build directories. Do not blanket-exclude frontend code from coverage to manufacture a higher score. Root Go coverage excludes the separate E2E module and does not measure web/site coverage. Reports are retained as run artifacts, not committed. Confirm the proposed Cloud project key when onboarding; no Cloud project or GitHub secret is created by this change.

### Regression verification

Use existing YAML helpers in tools/ci-plan for independent assertions of triggers, same-repository/Dependabot/manual restrictions, permissions, immutable pins, job dependencies, report upload/download identity, scanner secret scope, and Cloud properties. Execute the eligibility shell against empty and nonempty dummy tokens and assert enabled output/skip behavior without exposing token contents. Execute the coverage step with a fake go executable to prove command arguments, failure propagation, and expected report production without rerunning the application suite inside workflow-unit tests. Every new spec scenario receives its required test annotation. Record the expected red failures before implementation.

## Risks / Trade-offs

- Extra coverage runtime: coverage uses all root packages independently of the existing path-selected tests; retain its own bounded job budget and disclose the extra CI cost.
- Partial coverage: Go short-mode coverage is not full Go/E2E/frontend coverage; report this limitation rather than suppressing uncovered source.
- External onboarding: live analysis cannot be verified until the Cloud project exists and SONAR_TOKEN is configured; missing credentials visibly skip analysis, but a configured invalid token fails the scanner.
- Same-repository source remains able to change analysis configuration: follow the existing collaborator trust boundary; never extend it to fork PRs with a privileged trigger.

## Migration Plan

Create or import Adyen/agent-marketplace into the existing Cloud organization with the configured key, choose GitHub Actions analysis and disable automatic analysis if enabled, then configure the repository SONAR_TOKEN secret. Run main analysis to establish the baseline, verify PR decoration, and leave branch-protection requirements unchanged. Removing the standalone workflow disables this integration without affecting the existing gates.
