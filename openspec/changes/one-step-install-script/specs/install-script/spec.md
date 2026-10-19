## Purpose

Lets a user install the latest stable agent-marketplace release with one command, verified against the checksums the release publishes.

## ADDED Requirements

### Requirement: Stable releases publish binary checksums

Every stable release SHALL publish a `SHA256SUMS` asset next to the four platform binaries. It MUST contain exactly one SHA-256 entry per platform binary, in `sha256sum` format with the bare asset file name, and nothing else. The four binary assets and their names MUST remain unchanged, and the checksum asset name MUST NOT begin with `agent-marketplace-`.

#### Scenario: Release build writes checksums for the four binaries
- **WHEN** the release build produces the four platform binaries for a stable tag
- **THEN** it also produces `SHA256SUMS` listing each of the four binary file names with its SHA-256
- **AND** `sha256sum --check SHA256SUMS` succeeds in the release output directory

### Requirement: Install script selects a supported platform

The install script SHALL map the operating system to `darwin` or `linux` and the processor to `amd64` (`x86_64`, `amd64`) or `arm64` (`arm64`, `aarch64`). Any other operating system or processor MUST be refused with an error naming the unsupported value before anything is downloaded.

#### Scenario: Supported platforms select their release binary
- **WHEN** the script runs on macOS or Linux with an Intel or ARM 64-bit processor
- **THEN** it downloads `agent-marketplace-<tag>-<os>-<arch>` for that platform

#### Scenario: Unsupported platform is refused before download
- **WHEN** the script runs on an operating system or processor outside the supported set
- **THEN** it exits non-zero with an error naming the unsupported value
- **AND** it makes no download request and installs nothing

### Requirement: Install script resolves the release version

Without a pin, the install script SHALL install the release that `https://github.com/Adyen/agent-marketplace/releases/latest` redirects to, without using the GitHub API. `AGENT_MARKETPLACE_VERSION` SHALL pin an exact `vMAJOR.MINOR.PATCH` tag. A pin in any other form, or a latest release that cannot be resolved to such a tag, MUST be refused before any binary download.

#### Scenario: Latest stable release is installed by default
- **WHEN** the script runs without `AGENT_MARKETPLACE_VERSION`
- **THEN** it installs the binary of the release the latest-release URL redirects to

#### Scenario: Pinned version is installed
- **WHEN** `AGENT_MARKETPLACE_VERSION` is an exact stable tag
- **THEN** the script downloads that tag's binary and checksums without resolving the latest release

#### Scenario: Malformed or unresolvable version is refused
- **WHEN** `AGENT_MARKETPLACE_VERSION` is not an exact `vMAJOR.MINOR.PATCH` tag, or the latest release cannot be resolved to one
- **THEN** the script exits non-zero with an error before downloading a binary
- **AND** nothing is installed

### Requirement: Install script verifies the download before installing

The install script SHALL download the binary and the release's `SHA256SUMS` anonymously into a private temporary directory and verify the binary's SHA-256 with `sha256sum` or `shasum -a 256`. A failed download, a missing `SHA256SUMS`, a missing entry for the binary, a checksum mismatch, or the absence of both hash tools MUST fail with an error. On any failure the install directory MUST be unchanged, including any existing `agent-marketplace`, and the temporary directory MUST be removed.

#### Scenario: Verified download is installed
- **WHEN** the downloaded binary matches its `SHA256SUMS` entry
- **THEN** the script installs it

#### Scenario: Unverifiable download leaves the installation unchanged
- **WHEN** a download fails, `SHA256SUMS` is missing or lacks the binary's entry, the checksum does not match, or no hash tool is available
- **THEN** the script exits non-zero with an error naming the cause
- **AND** the install directory, including any existing `agent-marketplace`, is unchanged
- **AND** no temporary download remains

### Requirement: Install script installs into a user directory

The install script SHALL install the verified binary as `agent-marketplace` in `$HOME/.local/bin`, or in `AGENT_MARKETPLACE_INSTALL_DIR` when set, creating the directory when missing. The installed file MUST be executable and MUST replace an existing `agent-marketplace` atomically. The script SHALL print the installed program's `--version` output, and SHALL print how to add the directory to `PATH` when it is not already on `PATH`.

#### Scenario: Binary is installed into the default or overridden directory
- **WHEN** verification succeeds
- **THEN** an executable `agent-marketplace` exists in `$HOME/.local/bin`, or in `AGENT_MARKETPLACE_INSTALL_DIR` when set, with the downloaded bytes
- **AND** the script prints the installed program's version

#### Scenario: Existing installation is replaced
- **WHEN** the install directory already contains `agent-marketplace`
- **THEN** after a successful run it contains the newly verified binary

#### Scenario: PATH hint is shown only when needed
- **WHEN** the install directory is not on `PATH`
- **THEN** the script prints how to add it to `PATH`
- **AND** when the directory is already on `PATH` no such hint is printed

### Requirement: Public project website serves the install script

The public project website build SHALL publish the install script byte-for-byte at `install.sh` below its base path, so that the root-based deployment serves it at `https://agent-marketplace.io/install.sh`. The landing page install command SHALL be `curl -fsSL https://agent-marketplace.io/install.sh | bash`.

#### Scenario: Site build publishes the install script
- **WHEN** the public project website is built
- **THEN** `install.sh` below the site base is byte-identical to the install script source
- **AND** the landing page shows the `curl -fsSL https://agent-marketplace.io/install.sh | bash` install command
