## MODIFIED Requirements

### Requirement: Newer-version availability notice

The GUI SHALL decide whether its version is outdated by the build-kind rules of `upgrade-notice` "Newer releases are detected by build kind", comparing it with the latest stable release on the project's public GitHub Releases, and SHALL display a notice while a newer release is known. It SHALL make no release request and show no notice when `AGENT_MARKETPLACE_NO_UPDATE_CHECK` or `CI` is set to a non-empty value or the effective `update_check.enabled` setting under `upgrade-notice` is `false`. It SHALL NOT download an update until the user activates the notice's update action. A failed version check SHALL be silent. When the build has no package format, or the latest release publishes no asset for this build's package format and architecture, the notice SHALL offer only a link to the release page.

#### Scenario: New stable release is available

- **WHEN** release checking is enabled and the latest stable public GitHub release is newer than the running GUI version
- **THEN** the GUI SHALL display an availability notice naming the newer release
- **AND** it SHALL NOT download the release before the user activates the update action

#### Scenario: Version check fails

- **WHEN** release checking is enabled and the latest stable release cannot be checked
- **THEN** the GUI SHALL continue without an error notice or update download

#### Scenario: Organization disables release checking

- **WHEN** Consumer organization configuration sets `update_check.enabled: false` and user-personal configuration does not set it
- **THEN** the GUI SHALL make no network request for release availability
- **AND** it SHALL show no newer-version notice

#### Scenario: Update checking is suppressed

- **WHEN** `AGENT_MARKETPLACE_NO_UPDATE_CHECK` or `CI` is set to a non-empty value, or user-personal configuration sets `update_check.enabled: false`
- **THEN** the GUI SHALL make no network request for release availability
- **AND** it SHALL show no newer-version notice

#### Scenario: Unpackaged build links to the release page

- **WHEN** a newer release is known and the running GUI was not built as a macOS app, RPM, or DEB package
- **THEN** the notice SHALL offer a link to the release page and no in-app update

## ADDED Requirements

### Requirement: In-app update on macOS

When a newer release is known, the running GUI is a packaged macOS app that is not running from App Translocation or a mounted disk image, its containing directory is writable, and the release publishes the macOS `.zip` with a `SHA256SUMS` entry, the notice SHALL offer an update action. Activating it SHALL download that release's `.zip` and `SHA256SUMS`, verify the archive digest, and verify that the extracted app has the running app's bundle identifier, the release's version, and a valid code signature. The GUI SHALL then apply the "Quit confirms only unsynced or running work" rules before replacing itself; cancelling SHALL leave the running app unchanged. After an accepted quit it SHALL replace the running app with the verified app and launch the new version. A download, verification, or replacement failure SHALL leave the running app usable and unchanged, SHALL show an error in the notice, and SHALL allow the user to retry.

#### Scenario: User updates the app in place

- **WHEN** an eligible packaged macOS app shows a newer release and the user activates the update with no unsynced or running work
- **THEN** the GUI SHALL download and verify the release's `.zip`
- **AND** it SHALL replace the running app at the same location and launch the new version
- **AND** the new version SHALL report the release version

#### Scenario: Corrupted update download is rejected

- **WHEN** the downloaded `.zip` does not match its `SHA256SUMS` entry, or the extracted app has a different bundle identifier, a different version, or an invalid signature
- **THEN** the GUI SHALL NOT replace the running app
- **AND** it SHALL show an update error in the notice and offer a retry

#### Scenario: Pending work requires confirmation before the update

- **WHEN** the user activates the update while unsynced or running work exists
- **THEN** the GUI SHALL ask for the same confirmation as a quit
- **AND** cancelling SHALL leave the running app and its version unchanged

#### Scenario: Ineligible location falls back to a browser download

- **WHEN** a newer release is known and the packaged macOS app runs from App Translocation, a mounted disk image, or a directory it cannot write
- **THEN** the notice SHALL offer to open the release's `.dmg` download in the browser instead of an in-app update
- **AND** for App Translocation or a mounted disk image, it SHALL tell the user to move the app to Applications to enable updates

### Requirement: Verified package handoff on Linux

When a newer release is known, the running GUI was built as an RPM or DEB package, and the release publishes the package of that format for the running architecture with a `SHA256SUMS` entry, the notice SHALL offer an update action. Activating it SHALL download that package and `SHA256SUMS`, verify the package digest, save the package in the user's download directory, open it with the desktop's default handler, and ask the user to restart the GUI after installing. A package that fails verification SHALL be deleted and SHALL NOT be opened. When the handler cannot be opened, the notice SHALL show the saved package path and a link to the release page.

#### Scenario: User hands the verified package to the software installer

- **WHEN** an RPM- or DEB-packaged GUI shows a newer release and the user activates the update
- **THEN** the GUI SHALL save the verified package of the same format and architecture in the user's download directory
- **AND** it SHALL open the package with the desktop's default handler
- **AND** it SHALL tell the user to restart the GUI after installing

#### Scenario: Unverified Linux package is not opened

- **WHEN** the downloaded package does not match its `SHA256SUMS` entry
- **THEN** the GUI SHALL delete the download and SHALL NOT open it
- **AND** it SHALL show an update error in the notice and offer a retry
