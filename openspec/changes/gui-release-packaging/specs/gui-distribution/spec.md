## Purpose

Defines the desktop GUI packages published with each tagged release, their signing state, the checks proving they can be signed downstream, pull-request package builds, and the guarantee that CLI release artifacts stay unchanged.

## ADDED Requirements

### Requirement: Tagged releases publish GUI packages

Each tagged stable release SHALL publish, alongside the CLI artifacts, `agent-marketplace-gui-<tag>-macos-universal.dmg` and `agent-marketplace-gui-<tag>-macos-universal.zip` containing the same universal (`arm64` and `amd64`) macOS `.app`, and `agent-marketplace-gui-<tag>-linux-<arch>.rpm` and `agent-marketplace-gui-<tag>-linux-<arch>.deb` for `amd64` and `arm64`. The `.app` SHALL carry the bundle identifier `io.agent-marketplace.gui` and a version equal to the tag without its `v` prefix. Each Linux package SHALL install the GUI executable, a desktop entry, and an icon, and SHALL declare its WebKitGTK 4.1 and GTK 3 runtime dependencies. The release's `SHA256SUMS` SHALL list every GUI asset. GUI packages SHALL NOT contain or install the CLI.

#### Scenario: Tagged release publishes the GUI package matrix

- **WHEN** a tagged stable release is published
- **THEN** it SHALL include the macOS `.dmg` and `.zip` and the Linux `.rpm` and `.deb` for `amd64` and `arm64`
- **AND** the `.app` in the `.dmg` and the `.zip` SHALL be identical, contain both macOS architectures, and declare the release version and bundle identifier `io.agent-marketplace.gui`
- **AND** `SHA256SUMS` SHALL contain a line for each GUI asset

#### Scenario: Linux package installs a launchable desktop application

- **WHEN** a published `.rpm` is installed on a supported Fedora release or a published `.deb` is installed on Ubuntu 22.04, Ubuntu 24.04, or Debian 12 by the system package manager
- **THEN** the package manager SHALL resolve its WebKitGTK and GTK dependencies
- **AND** a desktop entry SHALL make the GUI appear in the desktop's application launcher
- **AND** no `agent-marketplace` CLI executable SHALL be installed by the package

#### Scenario: Missing GUI asset fails the release

- **WHEN** any expected GUI asset is missing from the packaging output of a release build
- **THEN** the release SHALL NOT be published

### Requirement: macOS app is ad-hoc signed with the hardened runtime

Until project code signing is configured by a separate change, the published `.app` SHALL be ad-hoc signed with the hardened runtime and its declared entitlements, and SHALL be signed with no signing identity, Apple team ID, or signing secret. Release and pull-request workflows SHALL NOT reference a signing identity, Apple team ID, signing-key location, or signing secret.

#### Scenario: Published app launches without a signing identity

- **WHEN** CI inspects and launches a built `.app`
- **THEN** its signature SHALL be ad-hoc with the hardened runtime flag set
- **AND** it SHALL launch under the hardened runtime and reach the GUI workspace

#### Scenario: Workflows contain no signing material

- **WHEN** release and pull-request workflows are configured
- **THEN** they SHALL NOT reference a signing identity, Apple team ID, signing-key location, or signing secret

### Requirement: Published GUI packages are downstream-signable

A distributor SHALL be able to re-sign the published `.app` with a Developer ID and the hardened runtime and submit it for notarization without rebuilding it, and SHALL be able to sign a published `.rpm` with `rpm --addsign` without rebuilding it. Every pull-request and release build SHALL prove this on copies of its artifacts: it SHALL re-sign a copy of the `.app` with the hardened runtime and pass `codesign --verify --strict --deep`, and SHALL generate a throwaway GPG key, sign a copy of each `.rpm` with `rpm --addsign`, and pass `rpm -K`. The throwaway key SHALL NOT be retained after the job, and the published artifacts SHALL NOT be modified by these checks.

#### Scenario: CI re-signs copies of the app and RPMs

- **WHEN** a pull-request or release build completes package creation
- **THEN** a re-signed copy of the `.app` SHALL pass `codesign --verify --strict --deep`
- **AND** a copy of each `.rpm` signed with a throwaway key SHALL pass `rpm -K`
- **AND** the throwaway key SHALL NOT be retained after the job
- **AND** the artifacts that are uploaded or published SHALL be byte-identical to those before the checks

### Requirement: Pull-request builds produce and smoke-test the GUI packages

Pull requests that change the GUI, its packaging, or release wiring SHALL build the same GUI package matrix as a tagged release, upload it as workflow artifacts, launch the macOS `.app`, and install and launch the Linux packages on the supported distributions.

#### Scenario: Pull request builds and launches every package

- **WHEN** a pull request changes the GUI, its packaging, or release wiring
- **THEN** CI SHALL produce every GUI asset of the tagged-release matrix and upload them as workflow artifacts
- **AND** CI SHALL launch the `.app` on macOS and install and launch the `.rpm` on Fedora and the `.deb` on Ubuntu 22.04, Ubuntu 24.04, and Debian 12

### Requirement: CLI release artifacts remain unchanged

GUI packaging SHALL leave the CLI release artifacts statically linked, with unchanged names and bytes, and SHALL leave their `SHA256SUMS` lines unchanged. GUI asset names SHALL NOT match the CLI asset pattern `agent-marketplace-<tag>-*`.

#### Scenario: Adding GUI packages does not change CLI artifacts

- **WHEN** a release publishes GUI packages alongside CLI artifacts
- **THEN** the CLI artifacts SHALL be statically linked and byte-identical to the output of the CLI release build
- **AND** their `SHA256SUMS` lines SHALL equal those produced by the CLI release build
- **AND** no GUI asset name SHALL match `agent-marketplace-<tag>-*`
