## REMOVED Requirements

### Requirement: Migrated selection workflows retain functional coverage

**Reason**: The shipped CLI has no consumer migration surface; successful in-process migration is not a supported prerequisite for regression workflows.

**Migration**: Retire the migrated-workflow scenario and retain its bulk/tag/ad-hoc, unrelated-selection preservation, synchronization, and drift-check assertions in the populated v4 replacement below.

## MODIFIED Requirements

### Requirement: Released clients consume candidate committed state

The released-client regression gate SHALL use a checksum-verified pinned stable v4 release in an isolated environment with separate user homes and repository checkouts. The shared consumer configuration SHALL be v4. That released client MUST successfully synchronize locked state and check synchronization after the candidate synchronizes, installs, updates, and removes resources in their shared repository. Plain candidate synchronization of a healthy committed acquisition lock and released-client locked/check reads MUST preserve its bytes. Independent native-hook source-layout verification and owned hook upgrade/deselection coverage SHALL remain exercised.

#### Scenario: Released client remains usable after candidate mutations

- **WHEN** the candidate synchronizes, installs, updates, and removes resources in a v4-configured repository shared with the pinned stable v4 release
- **THEN** the released client MUST pass locked synchronization and synchronization checking after each mutation
- **AND** the candidate's update and removal MUST change the intended materialized resource
- **AND** healthy committed lock bytes MUST remain unchanged through plain candidate synchronization and released-client locked/check reads

## ADDED Requirements

### Requirement: v4 selection workflows retain functional coverage

The regression suite SHALL exercise bulk, tag, and ad-hoc installation and removal against populated v4 configurations without requiring consumer migration. It MUST prove both the intended selection change and preservation of unrelated selected resources, including a subsequent synchronization and drift check. Changing fixture syntax MUST NOT retire functional selection coverage.

#### Scenario: v4 selection workflows remain usable

- **WHEN** a populated v4 installation installs and removes resources through bulk, tag, or ad-hoc selection
- **THEN** the intended resources MUST materialize and remain removed after subsequent synchronization
- **AND** unrelated selected resources MUST remain installed
- **AND** synchronization checking MUST succeed

### Requirement: Successful consumer command coverage is v4-only

Every public command and flag that interprets consumer configuration SHALL have a successful behavioral E2E invocation in a v4 consumer workflow. Every present consumer root in that invocation, including organization, MUST be exact v4; first-run workflows MAY create absent roots as v4. Unsupported-format refusal, a relabeled legacy invocation, or help-only execution MUST NOT satisfy successful operational coverage. Schema-independent commands and flags SHALL retain successful invocations without requiring consumer configuration. The coverage gate MUST fail when a public surface lacks its required successful invocation; it MUST NOT introduce uncovered-surface exceptions for this removal.

#### Scenario: Consumer command coverage requires v4 success

- **WHEN** the complete E2E suite evaluates a public consumer command or flag
- **THEN** the coverage gate MUST require an actual successful v4 workflow for that surface
- **AND** failure-only/refusal/help-only evidence, successful execution with an unsupported root, or a missing invocation MUST NOT count as coverage

#### Scenario: Schema-independent surfaces remain successfully exercised

- **WHEN** the E2E suite evaluates completion, help, harness conversion, or Marketplace operator commands that do not interpret consumer configuration
- **THEN** a successful behavioral invocation without a consumer root MUST satisfy that surface's coverage
- **AND** consumer-config v4 validation MUST NOT become a prerequisite for those independent operations

### Requirement: Launch gates witness refusal and supported interoperability

Launch acceptance SHALL witness unsupported consumer-root/include refusal through the normal Marketplace E2E gate and retain the pinned stable v4 released-client gate. It SHALL NOT require successful operation or in-process migration of a pre-v4 consumer installation, nor require a pre-v4 executable gate. Still-supported acquisition-lock/source-revision, scope, collision, and owned-entry preservation/removal behavior SHALL retain v4 regression coverage. The aggregate `E2E Gates` status MUST pass only when planning succeeds and both Marketplace and released-client gates succeed when selected or both are skipped because planning intentionally did not select them. Failed, cancelled, unexpectedly skipped, or unexecuted required witnesses MUST fail acceptance.

#### Scenario: Unsupported roots are witnessed by the launch gate

- **WHEN** launch acceptance runs the Marketplace E2E gate
- **THEN** its required witnesses MUST execute and pass unsupported-version/versionless refusal matrices for every consumer layer and explicit non-v4 included fragments
- **AND** a required refusal witness that skips, fails, or never runs MUST fail the gate
- **AND** supported v4 lock, scope, collision, and ownership regressions MUST remain exercised without requiring a pre-v4 executable

#### Scenario: Launch aggregate does not hide missing gates

- **WHEN** the aggregate `E2E Gates` status evaluates planning and its Marketplace/released-client results
- **THEN** selected gates MUST both have succeeded, or unselected gates MUST both have intentionally skipped after successful planning
- **AND** any failed planning, cancellation, missing execution, or unexpected skip MUST fail the aggregate rather than be accepted as a retired-upgrade-gate result
