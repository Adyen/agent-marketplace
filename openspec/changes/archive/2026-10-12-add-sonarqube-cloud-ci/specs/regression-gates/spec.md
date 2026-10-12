## ADDED Requirements

### Requirement: Public Cloud analysis preserves CI trust boundaries

Public CI SHALL submit eligible source revisions to SonarQube Cloud without referencing internal analysis infrastructure. Main-branch pushes and same-repository pull requests targeting main SHALL be eligible; manual analysis SHALL be limited to main. Fork and Dependabot pull requests MUST NOT receive analysis credentials. Missing analysis credentials SHALL produce an explicit onboarding skip rather than a fabricated analysis result. Available credentials MUST NOT be supplied to source build or test execution. Existing CI gates and branch-protection requirements MUST remain unchanged.

#### Scenario: Eligible revisions use the public service

- **WHEN** a main-branch push or same-repository pull request targeting main has analysis credentials available
- **THEN** its analysis SHALL use the public SonarQube Cloud service
- **AND** the submitted analysis SHALL identify the analyzed source revision
- **AND** public configuration MUST NOT reference internal analysis endpoints or execution infrastructure

#### Scenario: Untrusted requests receive no analysis credentials

- **WHEN** the request originates from a fork or Dependabot, or manual analysis targets a branch other than main
- **THEN** token-dependent analysis SHALL be skipped
- **AND** no privileged alternative trigger SHALL execute that request's code

#### Scenario: Missing credentials produce an explicit skip

- **WHEN** an otherwise eligible run has no analysis token configured
- **THEN** the run SHALL explain that analysis was skipped pending token setup
- **AND** it MUST NOT invoke the scanner or claim an analysis result

### Requirement: Cloud analysis consumes matching coverage evidence

Coverage evidence submitted with Cloud analysis SHALL be freshly generated from the same revision in the same workflow run, without analysis credentials in the build or test environment. Failed coverage generation, missing reports, and scanner failures MUST remain failures rather than successful analysis. Go coverage SHALL describe the root-module short-mode test run; analysis of frontend source MUST NOT be represented as measured frontend coverage without corresponding reports.

#### Scenario: Analysis imports fresh matching coverage

- **WHEN** an eligible revision completes root-module short-mode Go tests with coverage
- **THEN** analysis SHALL import the report generated for that revision in that workflow run
- **AND** no report from another run or revision SHALL be substituted
- **AND** source tests MUST NOT receive analysis credentials

#### Scenario: Analysis failures remain visible

- **WHEN** coverage generation fails, the required coverage report is missing, or the scanner fails
- **THEN** the affected workflow SHALL not report successful analysis
- **AND** scanner execution SHALL depend on successful coverage generation and receipt of its report
