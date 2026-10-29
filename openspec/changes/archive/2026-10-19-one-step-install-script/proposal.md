## Why

Installing the CLI today means working out the platform name, downloading the right release file by hand, marking it executable, and moving it onto `PATH`. New users need a single command they can copy from the landing page, and releases publish no checksums, so a scripted download has nothing to verify against.

## What Changes

- Stable releases publish a `SHA256SUMS` asset covering the four platform binaries, alongside the unchanged binary assets.
- A bash install script detects the platform, resolves the latest stable release (or a pinned tag), downloads the matching binary and `SHA256SUMS` anonymously, verifies the checksum, and installs `agent-marketplace` into a user-writable directory without `sudo`.
- The public project website serves the script at `install.sh` below its base path, so the root deployment serves `https://agent-marketplace.io/install.sh`.
- The landing page install command, the README, and the install guide lead with `curl -fsSL https://agent-marketplace.io/install.sh | bash`; manual downloads, `go install`, and source builds remain as further options.

## Capabilities

### New Capabilities
- `install-script`: release checksum publication and the one-step install script served by the public project website.

### Modified Capabilities

## Impact

- Release build (`make release`) and the release workflow's uploaded assets gain `SHA256SUMS`; Homebrew publication still downloads only the four binaries.
- New `site/public/install.sh`, published by the public site build; landing page and docs content.
- Releases before the first one that publishes `SHA256SUMS` cannot be installed with the script, and anonymous downloads work only while the repository is public.
