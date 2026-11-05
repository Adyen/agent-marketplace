## ADDED Requirements

### Requirement: Install from an unconfigured source

Pressing `i` in the resource list SHALL open an install-from-source modal with
three steps: Source, Pick, and Review. The Source step SHALL accept a Git URL,
a GitHub or GitLab web URL, or a directory path, plus an optional ref and
path; a web URL SHALL fill the ref and path it encodes. Before any network
access, the TUI SHALL reject a source that the effective allowlist blocks or
whose registry path is invalid, and SHALL show the rejection on the Source
step. The Pick step SHALL show the resources the source contains, grouped by
kind, with the commit the ref resolved to, counts per kind, and a status for
each resource: already installed, conflicting with an installed resource from
another source, or malformed. Malformed resources SHALL NOT be selectable. An
empty source SHALL show a hint and SHALL NOT advance to Review. A source that
expands into several bundles SHALL list the bundle paths and SHALL preview the
chosen bundle path. The Review step SHALL offer only the scopes the source may
be written to, SHALL name the configuration file that will be written, and
SHALL list the selected resources. Confirming SHALL write the source and the
selected resources to the chosen scope in one guarded mutation, with the same
configuration, acquisition lock, and harness results as `install --from` with
those names. The install SHALL refuse when the source no longer resolves to
the previewed commit. Previewing, cancelling, and every refusal SHALL leave
configuration, acquisition lock, and harness bytes unchanged. The modal SHALL
NOT open when the consumer configuration is unsupported or invalid, or while
another operation is active.

#### Scenario: Previewing a source writes nothing

- **WHEN** a user presses `i`, enters an unconfigured Git source, and the Pick
  step shows its resources with the resolved commit and counts per kind
- **THEN** configuration, acquisition lock, and harness bytes MUST be
  unchanged

#### Scenario: Picked resources install at the chosen scope

- **WHEN** a user picks a skill and a command from the previewed source,
  chooses project scope, and confirms
- **THEN** the repository configuration MUST record the source with both
  resources selected, the repository acquisition lock MUST record the source
  at the previewed commit, both resources MUST appear in the resource list,
  and a sync MUST materialize them in each target harness

#### Scenario: Blocked source is refused before network access

- **WHEN** a user enters a source that the effective allowlist blocks
- **THEN** the Source step MUST show the refusal, the source MUST NOT be
  fetched, and configuration, acquisition lock, and harness bytes MUST be
  unchanged

#### Scenario: Moved ref refuses the stale install

- **WHEN** the previewed source's ref advances to a new commit before the user
  confirms the install
- **THEN** the install MUST be refused with the previewed and current commits,
  the modal MUST return to the Pick step, and configuration, acquisition lock,
  and harness bytes MUST be unchanged

#### Scenario: Cancelling at any step preserves bytes

- **WHEN** a user cancels the modal from the Source, Pick, or Review step
- **THEN** the modal MUST close and configuration, acquisition lock, and
  harness bytes MUST be unchanged

#### Scenario: Malformed and empty sources are not installable

- **WHEN** a previewed source contains a malformed resource, or contains no
  discoverable resources
- **THEN** the malformed resource MUST be shown as not selectable, and an
  empty source MUST show a hint and MUST NOT advance to Review

#### Scenario: Source with several bundles asks for a bundle path

- **WHEN** a previewed source expands into several bundles
- **THEN** the Pick step MUST list the bundle paths instead of resources, and
  choosing one MUST preview that bundle path

#### Scenario: Install from source is refused while mutations are unavailable

- **WHEN** a user presses `i` while another operation is active or the
  consumer configuration is unsupported
- **THEN** the modal MUST NOT open, the TUI MUST explain the refusal, and
  configuration, acquisition lock, and harness bytes MUST be unchanged
