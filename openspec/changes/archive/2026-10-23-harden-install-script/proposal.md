## Why

Review of the one-step install script found gaps that the first change left open: a missing `HOME` was only noticed after the release lookup, a directory at the target path failed after downloads, a binary was downloaded even when `SHA256SUMS` had no entry for it, a release without checksums looked the same as a network failure, and a temporary directory containing a backslash could break verification with GNU `sha256sum`. Installs into a directory that another `agent-marketplace` on `PATH` shadows also went unreported. The landing command has also been reversed for the rollout: no release publishes `SHA256SUMS` yet, so `go install` remains the advertised command until a release ships checksums.

## What Changes

- The script resolves the install directory before any network request: `HOME` is required only for the default directory or a leading `~`, `~` expands to `$HOME`, the directory must be absolute, and a directory at `agent-marketplace` in that location is refused.
- The script downloads `SHA256SUMS` first, resolves the binary's entry, and only then downloads the binary. A release without `SHA256SUMS` is reported distinctly from a network failure, and the binary is not requested in either case.
- The script hashes from standard input so the temporary directory name cannot affect verification.
- After installing, the script warns on stderr when another `agent-marketplace` earlier on `PATH` shadows the installed binary.
- The public site publishes `install.sh` byte-for-byte. The landing page command is no longer part of this capability, so `go install` stays the primary install command in the quick start, README, and landing page until a release publishes checksums.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `install-script`: verification order and messages, install directory resolution, shadow warning, and the public site requirement.

## Impact

- `site/public/install.sh`, `scripts/install_script_test.go`, `tools/ci-plan` (the script change now selects the Go test job), and the install guide in `site/docs/install.md`.
- No change to release assets or to the `SHA256SUMS` format.
