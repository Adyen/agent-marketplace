## ADDED Requirements

### Requirement: Install-from-source recovers from a lock-pinned source

When the install-from-source modal's install is refused because the chosen
scope's acquisition lock pins the source at a commit other than the previewed
one, the modal SHALL return to the Review step with the picks and the chosen
scope kept, SHALL show the locked and previewed commits and the `update`
command that advances the source, and SHALL NOT preview again.

#### Scenario: Lock-pinned source returns to Review with update remediation

- **WHEN** a user picks resources from a source that is already configured
  and locked behind the remote in project scope, chooses project scope, and
  confirms
- **THEN** the modal MUST stay on the Review step showing the locked and
  previewed commits and the `update` command, MUST NOT start a preview, and
  configuration, acquisition lock, and harness bytes MUST be unchanged
