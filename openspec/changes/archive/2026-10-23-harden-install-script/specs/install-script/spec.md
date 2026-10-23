## MODIFIED Requirements

### Requirement: Install script verifies the download before installing

The install script SHALL download the release's `SHA256SUMS` first, and the binary only after `SHA256SUMS` has an entry for it, both anonymously into a private temporary directory. It SHALL verify the binary's SHA-256 with `sha256sum` or `shasum -a 256`, reading the file from standard input. A failed download, a missing `SHA256SUMS`, a missing entry for the binary, a checksum mismatch, a failing hash tool, or the absence of both hash tools MUST fail with an error. A release without a `SHA256SUMS` asset MUST be reported as such, distinct from a network failure. On any failure the install directory MUST be unchanged, including any existing `agent-marketplace`, and the temporary directory MUST be removed. The temporary directory's name MUST NOT affect verification. Before any download, the install directory MUST be resolved and checked, and no install directory is created by a failed verification.

#### Scenario: Verified download is installed
- **WHEN** the downloaded binary matches its `SHA256SUMS` entry
- **THEN** the script installs it

#### Scenario: Unverifiable download leaves the installation unchanged
- **WHEN** a download fails, `SHA256SUMS` is missing or lacks the binary's entry, the checksum does not match, a hash tool fails, a temporary file cannot be created, the binary cannot be copied, made executable, run, or moved into place, or no hash tool is available
- **THEN** the script exits non-zero with an error naming the cause
- **AND** the install directory, including any existing `agent-marketplace`, is unchanged
- **AND** no temporary download or staged file remains

#### Scenario: Checksums are fetched before the binary
- **WHEN** the script installs a release
- **THEN** it requests `SHA256SUMS` before it requests the binary
- **AND** it requests the binary only after `SHA256SUMS` has an entry for it

#### Scenario: Missing SHA256SUMS is reported without fetching the binary
- **WHEN** the release does not publish `SHA256SUMS`
- **THEN** the script exits non-zero with an error saying the release does not publish `SHA256SUMS`
- **AND** it makes no request for the binary

#### Scenario: Network failure is reported distinctly from missing SHA256SUMS
- **WHEN** the request for `SHA256SUMS` fails without an HTTP response
- **THEN** the script exits non-zero with an error saying the download could not reach the release
- **AND** the error does not say the release lacks `SHA256SUMS`

#### Scenario: Temporary directory name does not affect verification
- **WHEN** the temporary directory path contains a backslash
- **THEN** the script still verifies the binary and installs it

#### Scenario: Install directory is not created before verification
- **WHEN** a download or verification fails and the install directory does not exist
- **THEN** the script does not create the install directory

### Requirement: Install script installs into a user directory

The install script SHALL install the verified binary as `agent-marketplace` in `AGENT_MARKETPLACE_INSTALL_DIR` when set, otherwise in `$HOME/.local/bin`, creating the directory when missing. A leading `~` in `AGENT_MARKETPLACE_INSTALL_DIR`, meaning exactly `~` or `~/...`, MUST expand to `$HOME`. The directory MUST be an absolute path after expansion. `HOME` MUST be required only for the default directory or a `~` expansion, and that check MUST happen before any network request. A directory at the target `agent-marketplace` path MUST be refused before any network request. The installed file MUST be executable and MUST replace an existing `agent-marketplace` atomically. The script MUST run the staged binary with `--version` before replacing an existing `agent-marketplace`, and a failing run MUST leave the existing installation in place. The script SHALL print the installed program's `--version` output, and SHALL print how to add the directory to `PATH` when it is not already on `PATH`. When another `agent-marketplace` executable earlier on `PATH` shadows the installed one, the script SHALL warn on standard error.

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

#### Scenario: Home unset is refused for the default directory
- **WHEN** `HOME` is unset and `AGENT_MARKETPLACE_INSTALL_DIR` is unset or starts with `~`
- **THEN** the script exits non-zero with the error `HOME is not set`
- **AND** it makes no network request

#### Scenario: Absolute override needs no home
- **WHEN** `HOME` is unset and `AGENT_MARKETPLACE_INSTALL_DIR` is an absolute path
- **THEN** the script installs the binary in that directory

#### Scenario: Tilde override expands to home
- **WHEN** `AGENT_MARKETPLACE_INSTALL_DIR` is `~` or starts with `~/`
- **THEN** the script installs the binary in `$HOME` or `$HOME/...` accordingly

#### Scenario: Relative install directory is refused
- **WHEN** `AGENT_MARKETPLACE_INSTALL_DIR` is not an absolute path after expansion
- **THEN** the script exits non-zero with an error naming the value
- **AND** it makes no network request

#### Scenario: Directory at the target is refused
- **WHEN** the install directory already contains a directory named `agent-marketplace`
- **THEN** the script exits non-zero with an error naming that path
- **AND** it makes no network request

#### Scenario: Shadowing binary earlier on PATH is warned
- **WHEN** another `agent-marketplace` executable earlier on `PATH` takes precedence over the installed binary
- **THEN** the script prints a warning to standard error naming both paths

#### Scenario: No shadow warning when the install directory resolves first
- **WHEN** the install directory's `agent-marketplace` is the one `PATH` resolves
- **THEN** the script prints no shadow warning

### Requirement: Public project website serves the install script

The public project website build SHALL publish the install script byte-for-byte at `install.sh` below its base path, so that the root-based deployment serves it at `https://agent-marketplace.io/install.sh`. The landing page install command is not specified by this capability.

#### Scenario: Site build publishes the install script
- **WHEN** the public project website is built
- **THEN** `install.sh` below the site base is byte-identical to the install script source
