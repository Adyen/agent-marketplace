## ADDED Requirements

### Requirement: Lock-pinned sources are not reported as moved

When an explicit-source install carries an expected commit, and the target
layer's existing acquisition lock already pins the source's URL and ref at a
different commit that the install keeps, the install MUST fail before
publishing configuration or acquisition lock changes or materializing
anything. The refusal MUST be distinguishable from a moved source, MUST NOT
report that the source moved, and MUST name the target scope, the locked
commit, the expected commit, and an `update` command that advances the
source in that scope.

#### Scenario: Lock-pinned source refuses an expected-commit install with update remediation

- **WHEN** an explicit-source install into a layer whose acquisition lock
  pins the source's ref at an older commit carries the commit the ref
  currently resolves to as its expected commit
- **THEN** the install MUST fail naming the scope, both commits, and the
  `update` command, MUST NOT report that the source moved, and MUST leave
  configuration, acquisition locks, and materialized resources unchanged
