## 1. Package build

- [ ] 1.1 Add the app icon derived from `site/public/brand/favicon.svg` (`.icns` and hicolor PNG sizes), `Info.plist` (`Agent Marketplace`, `io.agent-marketplace.gui`, tag version, `LSMinimumSystemVersion` 12.0), and an empty hardened-runtime entitlements file; verify `plutil -lint` passes.
- [ ] 1.2 Add a package-format build variable (`macos-app`, `rpm`, `deb`, empty by default) set through `-ldflags`; verify plain `go build` reports no package format.
- [ ] 1.3 Add a make target that builds `darwin/arm64` and `darwin/amd64` with cgo, combines them with `lipo`, assembles and ad-hoc signs the `.app` with the hardened runtime, and writes the `.dmg` (with an `Applications` link) and the `ditto` `.zip`; verify `lipo -archs`, `codesign -dv` (ad-hoc, runtime), and that the `.dmg` and `.zip` contain identical apps.
- [ ] 1.4 Add one nFPM config and a make target that build the GUI with `webkit2_41` and produce the `.rpm` and `.deb` for the host architecture with the executable, desktop entry, icons, and WebKitGTK 4.1 and GTK 3 dependencies; verify package metadata and contents, and that no `agent-marketplace` CLI executable is included.

## 2. CI and release

- [ ] 2.1 Add GUI packaging jobs on macOS, `ubuntu-22.04`, and `ubuntu-22.04-arm` runners for pull requests that change the GUI, packaging, or release wiring and for release tags; upload the assets; add a matrix-completeness check that fails on any missing asset.
- [ ] 2.2 Add the macOS launch smoke test under the hardened runtime and Linux install-and-launch tests (Xvfb) for the `.rpm` on Fedora and the `.deb` on Ubuntu 22.04, Ubuntu 24.04, and Debian 12.
- [ ] 2.3 Add the signability checks on copies: re-sign the `.app` and pass `codesign --verify --strict --deep`; sign each `.rpm` with a throwaway GPG key and pass `rpm -K`; delete the key; verify the uploaded artifacts' digests are unchanged by the checks.
- [ ] 2.4 Make the `release` job depend on the GUI packaging jobs, append the GUI lines to `SHA256SUMS`, and publish the GUI assets; add a check that the CLI asset names, bytes, and `SHA256SUMS` lines equal `make release` output and that no GUI asset name matches `agent-marketplace-<tag>-*`.
- [ ] 2.5 Audit the workflows and tracked configuration for signing identities, Apple team IDs, signing-key locations, and signing secrets; verify none are present.

## 3. Updates

- [ ] 3.1 Remove `gui.version_check` (`internal/config/gui.go`, its merge and warning paths, generated API types, and tests) and drive the GUI release check from `AGENT_MARKETPLACE_NO_UPDATE_CHECK`, `CI`, and `config.UpdateCheckEnabled`; remove the removed requirement's `config-layering` annotations from `internal/config/gui_test.go`, `internal/localapi/release_test.go`, and `gui/tests/releases.spec.ts`; verify a declared `gui` key is rejected as an unknown field and every suppression input stops the request.
- [ ] 3.2 Apply the `upgrade-notice` build-kind rules to the GUI version and extend the local release endpoint to return the release page URL and, when published, this build's asset URL and `SHA256SUMS` URL; verify pseudo-version, `dev`, missing-asset, and prerelease cases.
- [ ] 3.3 Implement the macOS updater: eligibility (packaged, not translocated, not on a disk image, writable parent), download, digest check, staging on the same volume, bundle identifier, version, and `codesign --verify` checks, quit-rule confirmation, backup-and-swap with restore, relaunch after exit, and backup cleanup on the next start; verify success, every rejection path, cancel, and rename failure with fixtures.
- [ ] 3.4 Implement the Linux handoff: download the package for this format and architecture, verify it, save it in the XDG download directory, open it with `xdg-open`, ask for a restart, delete unverified downloads, and fall back to the saved path and release page when opening fails; verify each path.
- [ ] 3.5 Update the GUI notice: update action, progress, error with retry, browser fallback with the move-to-Applications hint, Linux restart hint, and release-page link for unpackaged builds; cover them in `gui/src` unit tests and `gui/tests` browser tests, moving existing `gui-shell` and `config-layering` scenario annotations to the new scenarios.
- [ ] 3.6 Prove an end-to-end macOS update in CI: install a packaged build of the pull request at version N from a local release stub, update it to N+1, and verify the relaunched app reports N+1.

## 4. Landing page and documentation

- [ ] 4.1 Add the landing-page GUI download area next to the unchanged CLI install command: look up the latest stable release at page load, build links from its asset names, choose the primary action by visitor OS, label Linux links by format and architecture, fall back to the latest-release page, and show the first-launch note; cover lookup success, failure, missing assets, and each OS in `site/tests`.
- [ ] 4.2 Add a desktop-app section to `site/docs/install.md` with the downloads, first-launch instructions, update behavior, and supported Linux distributions; replace `gui.version_check` with `update_check.enabled` in configuration reference pages.

## 5. Finalization

- [ ] 5.1 Run the package, release, GUI, and site checks and `make spec-check`; require zero trace findings for this change, with no unbound scenarios or dangling annotations.
- [ ] 5.2 Verify no Windows, AppImage, Flatpak, hosted repository, URL-scheme, or bundled-CLI artifacts enter the package matrix.
- [ ] 5.3 Run `openspec archive gui-release-packaging --yes` in this pull request.
