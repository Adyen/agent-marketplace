## ADDED Requirements

### Requirement: Resource detail separates reports from Marketplace policy

The resource detail Security tab MUST show the Marketplace policy outcome separately from individually attributed tool reports. Each report MUST identify the tool, available version, findings, and known coverage status. The page MUST NOT turn an advisory score, recommendation, failure, or missing report into a Marketplace verdict.

#### Scenario: Readers can compare tool reports

- **WHEN** the resource detail contains built-in findings and a partial advisory report with findings
- **THEN** the Security tab MUST show each tool's findings under its own name, label the advisory report partial, and preserve the Marketplace policy outcome
- **AND** existing built-in source-range navigation MUST remain usable

#### Scenario: Legacy or unavailable SkillSpector reports are not shown as clean

- **WHEN** a skill's assessment has no SkillSpector result or records a failed attempt
- **THEN** the Security tab MUST retain the built-in report and distinguish the absence of a scan result from a failed scan
- **AND** neither state MUST be represented as no findings

#### Scenario: Non-skill resources retain their applicable analysis

- **WHEN** a reader opens the Security tab of a non-skill resource with only built-in findings
- **THEN** the tab MUST show its Marketplace analysis without claiming that SkillSpector analyzed it
