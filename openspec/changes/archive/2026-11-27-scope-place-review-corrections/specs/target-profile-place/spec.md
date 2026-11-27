## MODIFIED Requirements

### Requirement: Initial-load verification results are recorded per generation

After successful publication, ownership commit and pruning finish for all approved Place work admitted to a sync invocation, sync SHALL run the approved initial-load checks. If publication, ownership commit or pruning fails for any admitted Place root, no initial-load check from that invocation SHALL run. A selected target skipped as unusable is not admitted and MUST NOT block the barrier. After the journal clears, sync MUST record each completed check result in one ownership-state update: pass or fail against the exact profile, package generation, check identity and host version. A committed generation with no recorded result is verification-pending.

#### Scenario: Crash recovery leaves verification pending, distinct from edits

- **WHEN** recovery completes a pending publication, and separately a user later edits an installed file
- **THEN** recovery MUST leave verification pending, and status MUST report the edit as drift, without labeling either event as the other

#### Scenario: A failed Place root defers checks for admitted work

- **GIVEN** one sync invocation admits Place work for roots A and B
- **WHEN** publication for B fails while A's publication could otherwise complete
- **THEN** no approved initial-load check from that invocation MUST run, and A MUST remain verification-pending

#### Scenario: Completed check outcomes preserve pass and fail evidence

- **GIVEN** all admitted Place roots publish, commit ownership and prune successfully
- **WHEN** one approved initial-load check fails and another passes
- **THEN** the checks MUST run only after the admitted work is complete, and status MUST retain the failed and passed results against their exact generations

#### Scenario: A skipped unusable target does not block the admitted-work barrier

- **GIVEN** one admitted Place root has an eligible failed or pending check and another selected target is skipped as unusable
- **WHEN** sync runs
- **THEN** the admitted root's check MUST run and the skipped target MUST NOT block it
