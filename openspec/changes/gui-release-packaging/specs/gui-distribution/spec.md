## Purpose

Defines unsigned desktop packages published for supported macOS and Linux architectures and the checks proving organizations can sign those packages downstream without changing the CLI release.

## ADDED Requirements

### Requirement: Unsigned tagged GUI release packages

Tagged releases SHALL publish an unsigned `.dmg` for each supported macOS architecture and an unsigned `.rpm` for each supported Linux architecture. Each `.dmg` SHALL contain an `.app` built for the hardened runtime with the entitlements needed for notarization. Each `.rpm` SHALL declare its WebKitGTK runtime dependency and install a desktop entry for the application. Checksums SHALL be published alongside the artifacts as for existing CLI artifacts.

#### Scenario: Tagged release publishes platform packages and checksums

- **WHEN** a tagged release is published
- **THEN** each supported macOS architecture SHALL have an unsigned `.dmg` containing the hardened-runtime `.app` with its notarization entitlements
- **AND** each supported Linux architecture SHALL have an unsigned `.rpm` declaring WebKitGTK and installing its desktop entry
- **AND** checksums SHALL be published alongside the GUI artifacts

### Requirement: Published GUI packages are downstream-signable

A distributor SHALL be able to sign and notarize the published `.app` without rebuilding it, using Developer ID `codesign` with the hardened runtime followed by notarization. A distributor SHALL be able to sign the published `.rpm` using `rpm --addsign`.

#### Scenario: Distributor signs the published app without rebuilding

- **WHEN** a distributor signs a published `.app` using Developer ID `codesign` with the hardened runtime and submits it for notarization
- **THEN** the `.app` SHALL be signable and notarizable without rebuilding it

#### Scenario: Distributor signs the published RPM

- **WHEN** a distributor signs a published `.rpm` using `rpm --addsign`
- **THEN** the RPM signature SHALL be applied without rebuilding the package

### Requirement: CI verifies downstream signability

Every pull-request and tagged-release build SHALL verify downstream signability of its GUI artifacts. CI SHALL ad-hoc sign a copy of the `.app` with the hardened runtime and pass `codesign --verify --strict`. CI SHALL generate a throwaway GPG key, sign a copy of the `.rpm` with `rpm --addsign`, and pass `rpm -K`.

#### Scenario: Every build validates app and RPM signing

- **WHEN** a pull-request or tagged-release build completes package creation
- **THEN** CI SHALL ad-hoc sign the `.app` with the hardened runtime and verify it using `codesign --verify --strict`
- **AND** CI SHALL generate a throwaway GPG key, sign the `.rpm` with `rpm --addsign`, and verify it using `rpm -K`
- **AND** the throwaway key SHALL NOT be retained after the CI job

### Requirement: Repository contains no organization-specific signing data

The repository and its workflows SHALL contain no organization-specific signing identities, Apple team IDs, signing-key locations, or signing secrets.

#### Scenario: No organization signing material is stored in the repository

- **WHEN** release and pull-request workflows are configured
- **THEN** no organization-specific signing identity, Apple team ID, signing-key location, or signing secret SHALL be stored in the repository or its workflows

### Requirement: Pull-request builds publish the same unsigned packages

Pull-request builds SHALL produce the same unsigned GUI package types and supported-architecture matrix as tagged releases and SHALL run a launch smoke test.

#### Scenario: Pull-request artifacts remain unsigned and launch

- **WHEN** a pull-request build publishes GUI artifacts
- **THEN** it SHALL produce the same unsigned `.dmg` and `.rpm` package types for supported architectures as a tagged release
- **AND** it SHALL run a launch smoke test

### Requirement: CLI release artifacts remain unchanged

GUI release packaging SHALL leave the CLI release artifacts statically linked and unchanged.

#### Scenario: Adding GUI releases does not change CLI artifacts

- **WHEN** a release publishes GUI packages alongside CLI artifacts
- **THEN** the CLI artifacts SHALL remain statically linked and unchanged by GUI packaging
