## Why

The desktop GUI is for people who do not use a terminal, such as product managers. They cannot build it with `go install` or install it with `install.sh`; they need a package they can download from agent-marketplace.io or GitHub Releases, open with a double click, and keep current without help. Project code signing is being arranged separately, so the first packages must run and update without a signing identity while staying ready for signing on public CI later.

## What Changes

- Tagged releases publish GUI packages alongside the unchanged CLI artifacts: one universal macOS `.dmg` for first installs, a universal macOS `.zip` of the same app for in-app updates, and an `.rpm` and a `.deb` for each supported Linux architecture. Every GUI asset is listed in the release's `SHA256SUMS`.
- The macOS `.app` is ad-hoc signed with the hardened runtime and its entitlements, so it launches on Apple Silicon and can later be re-signed with a Developer ID without a rebuild. No signing identity or secret is used yet.
- The GUI and the CLI stay separate products. GUI packages contain only the GUI; CLI users keep `install.sh` and `go install`.
- **BREAKING (unreleased setting)**: the GUI's update notice offers to install the update. On macOS the GUI downloads, verifies, replaces, and relaunches itself; on Linux it downloads and verifies the new package and opens it in the desktop's software installer. The organization-only `gui.version_check` setting is removed before it ships in a release; the GUI follows the same `update_check.enabled` setting and suppression rules as the CLI and TUI.
- The public landing page offers GUI downloads for macOS and Linux next to the unchanged CLI install command, with first-launch instructions while the app is unsigned.
- Pull-request builds produce the same packages, run launch and install smoke tests, and prove the packages can be re-signed downstream.

## Capabilities

### New Capabilities

- `gui-distribution`: GUI release packages, ad-hoc signing and downstream signability, checksums, pull-request package builds, and unchanged CLI release artifacts.

### Modified Capabilities

- `gui-shell`: the newer-version notice follows `update_check` and offers an in-app update on macOS and a verified package handoff on Linux.
- `config-layering`: remove the organization-only `gui.version_check` setting.
- `site-deployment-profiles`: the public landing page offers GUI downloads.

## Impact

Release and pull-request CI gain macOS and Linux GUI packaging jobs on native runners; the release job waits for them. `cmd/agent-marketplace-gui`, `internal/localapi` release handling, `internal/config` GUI settings, the GUI frontend notice, the public site landing page, and install documentation change. CLI binaries, their names, and their `SHA256SUMS` lines, `install.sh`, and the Homebrew tap are unaffected. Developer ID signing, notarization, Windows, AppImage, Flatpak, hosted package repositories, URL-scheme registration (`gui-install-deep-link`), and shipping the CLI inside GUI packages are out of scope.

## Release note

Agent Marketplace now ships a desktop app for macOS and Linux. Download it from agent-marketplace.io or GitHub Releases; it tells you when a new version is out and installs the update for you.
