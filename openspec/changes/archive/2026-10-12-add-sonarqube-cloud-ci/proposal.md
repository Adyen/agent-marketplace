## Why

Agent Marketplace needs Sonar code-quality feedback without exposing internal SonarQube infrastructure through its public CI. Adyen/adyen-flutter already uses SonarQube Cloud in the public `adyen` organization, providing an applicable onboarding pattern.

## What Changes

- Add a GitHub-hosted SonarQube Cloud workflow for pushes to main, pull requests targeting main, and manual verification on main.
- Configure the public Cloud organization `adyen` and proposed project key `Adyen_agent-marketplace`; confirm the key during project onboarding.
- Generate fresh root-module Go coverage in a credential-free job and pass it to analysis through an artifact from the same workflow run and revision.
- Skip token-dependent work for fork and Dependabot pull requests and report a clear onboarding skip when `SONAR_TOKEN` is absent.
- Keep internal endpoints, images, templates, and credentials out of the public configuration. Do not change branch-protection requirements or existing CI gates.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `regression-gates`: Add public Cloud analysis with explicit eligibility, secret handling, report provenance, and failure behavior.

## Impact

Add `.github/workflows/sonar_cloud.yml`, `sonar-project.properties`, and focused workflow regression tests under `tools/ci-plan`. Use the maintained SonarSource scan action pinned to an immutable revision. No application behavior, Go module dependencies, or internal infrastructure changes. Initial coverage is root-module Go short-mode coverage; web and site source analysis does not imply frontend coverage. The maintainer must create the Cloud project, select CI-based analysis, and add the `SONAR_TOKEN` GitHub Actions secret before a live scan can be verified.
