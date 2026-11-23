## ADDED Requirements

### Requirement: Configured analyzers report independently

Marketplace publication MUST retain the built-in security findings and report the result of each enabled supplementary skill analyzer separately for the exact skill revision. Supplementary findings MUST NOT influence the Marketplace policy decision or change intrinsic resource, archive, or inspection projection identity.

#### Scenario: Both tools report for mirrored and index-only skills

- **WHEN** Marketplace publishes a skill with SkillSpector enabled and both tools finish
- **THEN** the resource assessment MUST expose separately attributed built-in and SkillSpector results in both acquisition modes
- **AND** the existing policy decision and intrinsic digests MUST match publication with only the built-in analyzer

#### Scenario: Supplementary analysis cannot weaken rejection

- **WHEN** built-in analysis rejects a skill and the supplementary analyzer reports no findings
- **THEN** the resource assessment MUST remain rejected and distribution MUST continue to follow the existing reject policy

### Requirement: Incomplete analysis is distinct from no findings

Supplementary reports MUST distinguish complete, complete with caveats, partial, failed, and not-run analysis. A supplementary timeout, invalid result, unavailable runner, or exhausted scan budget MUST NOT prevent otherwise valid publication or appear as a scan with no findings.

#### Scenario: Partial report preserves supported findings

- **WHEN** a supplementary scanner completes with useful findings and explicit coverage gaps
- **THEN** its report MUST retain bounded findings and identify the coverage gap without changing the built-in result

#### Scenario: Supplementary scanner fails or cannot start

- **WHEN** an attempted supplementary scan times out, is unavailable, or returns invalid output, or an enabled scan cannot start within its aggregate budget
- **THEN** Marketplace MUST publish the untouched built-in outcome and distinguish a failed attempt from work not run
