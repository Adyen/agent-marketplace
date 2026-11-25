## Context

See proposal.md for motivation. The GUI is a Wails v2 cgo application (`cmd/agent-marketplace-gui`, built by `make gui-build` with `-tags desktop,production`). The CLI release is built by `make release` with `CGO_ENABLED=0` on one Linux runner and published by the `release` job with `softprops/action-gh-release`; a follow-up job publishes the Homebrew tap from assets matching `agent-marketplace-$TAG-*`, and `install.sh` looks up its asset by exact name in `SHA256SUMS`. The GUI already shows a newer-version notice from `internal/localapi/release.go`, governed by the organization-only `gui.version_check` setting, which has not shipped in any release.

The audience does not use a terminal. Project signing is being arranged with IT and will run on public CI in a later change.

## Goals / Non-Goals

**Goals:** Double-click installable GUI packages on GitHub Releases and the landing page; GUI updates without a terminal; packages that launch today without a signing identity and accept Developer ID signing later without a rebuild; no change to CLI artifacts or CLI install paths.

**Non-Goals:** Developer ID signing and notarization, an update-signing key, Windows, AppImage, Flatpak, hosted dnf/apt repositories, unattended background updates, URL-scheme registration, and bundling or installing the CLI with the GUI.

## Decisions

### Packages and names

| Asset | Purpose |
|---|---|
| `agent-marketplace-gui-<tag>-macos-universal.dmg` | First install: the app plus an `Applications` link to drag onto |
| `agent-marketplace-gui-<tag>-macos-universal.zip` | In-app update payload: the same `.app`, archived with `ditto -c -k --keepParent` so the signature and symlinks survive |
| `agent-marketplace-gui-<tag>-linux-<arch>.rpm` | Linux, `amd64` and `arm64` |
| `agent-marketplace-gui-<tag>-linux-<arch>.deb` | Linux, `amd64` and `arm64` |

The `agent-marketplace-gui-` prefix keeps GUI assets out of the Homebrew job's `agent-marketplace-$TAG-*` pattern and away from `install.sh`'s exact-name lookup. All GUI assets are added to the existing `SHA256SUMS`; existing CLI lines are unchanged.

- **macOS universal binary.** Build `darwin/arm64` and `darwin/amd64` with cgo on a macOS runner and combine them with `lipo`. One download avoids asking non-technical users to choose a chip, which browsers cannot detect reliably. Deployment target and `LSMinimumSystemVersion` are macOS 12.
- **Bundle identity.** Display name `Agent Marketplace`, bundle identifier `io.agent-marketplace.gui`, `CFBundleShortVersionString` and `CFBundleVersion` equal to the tag without `v`. The identifier must stay stable across releases because the updater compares it and later signing binds it.
- **App icon.** Derived from `site/public/brand/favicon.svg` into `.icns` and hicolor PNG sizes, committed with the packaging sources.
- **Linux.** Build natively on `ubuntu-22.04` and `ubuntu-22.04-arm` runners (glibc 2.35 baseline) with the `webkit2_41` tag against WebKitGTK 4.1. Package with nFPM from one config producing both formats. Install `/usr/bin/agent-marketplace-gui`, `/usr/share/applications/agent-marketplace-gui.desktop`, and hicolor icons. Declare WebKitGTK 4.1 and GTK 3 runtime dependencies using names that resolve on Fedora, Ubuntu 22.04/24.04, and Debian 12; CI installs the packages on those distributions to prove it. Distributions that ship only WebKitGTK 4.0 (for example RHEL 9) are unsupported.
- **Package format marker.** Each build records how it was packaged (`macos-app`, `rpm`, `deb`, or none) through `-ldflags`, so the updater knows which asset to fetch and plain `go build`/`go install` builds never self-update.

### Signing now, and later

- **Ad-hoc signing.** Apple Silicon refuses code without a valid signature and reports a modified bundle as damaged. The published `.app` is therefore signed with `codesign --force --options runtime --entitlements <file> -s -`. This uses no identity or secret. A first launch from a browser download still needs System Settings → Privacy & Security → Open Anyway, which the landing page explains.
- **Entitlements.** The hardened-runtime entitlement file starts empty; the launch smoke test runs the app under the hardened runtime and fails if a missing entitlement stops it. Entitlements are added only when that test proves the need.
- **Downstream signability.** Every pull-request and release build re-signs a copy of the `.app` with `codesign --force --options runtime` and passes `codesign --verify --strict --deep`, and signs a copy of each `.rpm` with a throwaway GPG key and passes `rpm -K`. `.deb` files are conventionally signed at repository level, so there is no per-package `.deb` signing check.
- **Later change.** Developer ID signing and notarization on public CI, with secrets in the CI secret store, replace the ad-hoc signature in a separate change once IT settles the identity. That change also makes the updater require the new bundle's Team ID to match the running app.

### Updates

- **One setting.** The GUI follows `upgrade-notice`: the build-kind rules decide whether a version is outdated (`dev`, dirty, and `git describe` builds are never checked), and `AGENT_MARKETPLACE_NO_UPDATE_CHECK`, `CI`, and the effective `update_check.enabled` (organization and user-personal configuration, user wins) suppress checking. `gui.version_check` and `internal/config/gui.go` are removed. They never shipped in a release, so removal needs no migration; an organization file that still sets `gui` is rejected like any unknown field.
- **User-initiated.** The GUI checks when it starts and shows the notice persistently while a newer release is known. Nothing downloads until the user activates the update. Background downloading can be added later without changing the trust model.
- **macOS in-place update.** Eligible when the build is `macos-app`, the running bundle is not under App Translocation and not on a mounted disk image, its parent directory is writable, and the release publishes the macOS `.zip` with a `SHA256SUMS` entry. The GUI then:
  1. downloads the `.zip` and `SHA256SUMS` for that release tag over HTTPS and verifies the archive digest;
  2. extracts it into a staging directory on the same volume as the running bundle and checks that the staged bundle's identifier equals the running one and its version equals the release tag, and that `codesign --verify --strict` accepts it;
  3. applies the existing quit rules (confirmation when unsynced or running work exists); cancelling leaves the running app and staged files untouched apart from cleanup;
  4. renames the running bundle to a backup, renames the staged bundle into place, restores the backup if the second rename fails, then exits and relaunches the new bundle from a helper that waits for this process to exit, so the single-instance lock is free;
  5. the new version deletes the backup on its first successful start.

  Files the GUI downloads itself carry no quarantine attribute, so updates do not repeat the first-launch Gatekeeper prompt. Until signing exists, integrity rests on HTTPS plus `SHA256SUMS` from the same release, the same trust model as `install.sh`.
- **macOS fallback.** When the app is not eligible, the notice offers a download that opens the release's `.dmg` in the browser. Under App Translocation or on a disk image, it also says to move the app to Applications to enable updates.
- **Linux handoff.** For `rpm` and `deb` builds, the GUI downloads the matching package for its architecture and `SHA256SUMS`, verifies it, saves it in the user's download directory (`XDG_DOWNLOAD_DIR`, falling back to `~/Downloads`), and opens it with the desktop's default handler (`xdg-open`), which is normally the software installer and asks for an administrator password. When opening fails, the GUI shows the saved file path and offers the release page. A package-manager install replaces the files under the running process, so the GUI asks the user to restart it afterwards.
- **Other builds.** Builds without a package format show the notice with a link to the release page only.
- **Release lookup.** The release endpoint keeps using the GitHub latest-release API and additionally returns the download URLs of the assets this build can use. A release that lacks this platform's asset degrades to the release-page link.

### Landing page

- The public profile's landing page adds a GUI download area beside the unchanged CLI `CommandBar`. At page load it reads the latest stable release from the GitHub API and builds links from that release's published asset names, so there are no alias assets and no window where a link points to an asset the latest release lacks. When the lookup fails, is rate limited, or the latest release has no GUI assets, it links to the latest-release page instead.
- macOS visitors see the `.dmg` as the primary action; Linux visitors see the `.rpm` and `.deb` links with architecture labels; others see all. A short first-launch note explains Open Anyway while the app is ad-hoc signed.
- The install documentation gains a desktop-app section with the same links and instructions.

### CI and release

- Pull requests that touch the GUI, packaging, or release wiring build the full package matrix, upload it as workflow artifacts, and run: a launch smoke test of the `.app` on macOS; install and Xvfb launch of the `.rpm` on Fedora and the `.deb` on Ubuntu 22.04, Ubuntu 24.04, and Debian 12; the signability checks; and a matrix-completeness check that fails when any expected asset is missing.
- The release job depends on the GUI packaging jobs, downloads their artifacts, appends their lines to `SHA256SUMS`, and publishes them with the CLI assets. CLI build steps are not modified; a check compares the CLI asset names and their `SHA256SUMS` lines with `make release` output to prove they are unchanged.
- No job uses a signing identity, Apple team ID, or signing secret.

## Risks / Trade-offs

- [First launch from a browser download is blocked until Open Anyway] → Landing-page and documentation instructions; Developer ID signing and notarization follow in a separate change.
- [A tampered release or compromised GitHub account could ship a malicious update] → Same exposure as `install.sh` today. The signing change adds Team ID verification to the updater.
- [macOS runners and three extra package jobs lengthen release and GUI pull-request CI] → Path filters limit pull-request runs; packaging jobs run in parallel with existing gates.
- [A GUI packaging failure blocks the whole release] → Accepted: the landing page and updater depend on every release carrying GUI assets. The matrix-completeness check catches gaps on pull requests first.
- [Linux distributions without WebKitGTK 4.1] → Declared unsupported; package dependencies make the installer refuse instead of producing a broken app.
- [The updater replaces a bundle the user launched from a read-only or translocated location] → Eligibility checks route those cases to the browser fallback.
- [Removing `gui.version_check` rejects an organization file that adopted it from pre-release builds] → It never shipped in a release; the error names the field.
