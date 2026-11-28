# Proposal

## Why

The maintainers have agreed to support Windows as a target platform for the agent-marketplace CLI. Today the CLI is built and released only for macOS and Linux, and several runtime guarantees are defined or implemented only for Unix-like hosts: serialized ownership and dependency mutations, symlink materialization, machine configuration locations, organization trust, and owner-only file permissions. A Windows build today would silently lose concurrency protection, refuse every file on POSIX mode-bit checks, and could check out registry content with different line endings than Linux.

## What Changes

- Publish Windows CLI binaries for `amd64` and `arm64` with every stable release, listed in the CLI release `SHA256SUMS`.
- Define where machine-scope configuration, acquisition locks, state, cache, and harness machine roots live on Windows. Skip Devin machine scope on Windows with a warning until its Windows root is confirmed.
- Read the consumer organization configuration from the machine's program-data folder, and apply it only when administrator-owned and not writable by other users.
- Map owner-only permission requirements to Windows access control lists.
- Keep every filesystem operation below an authorized root bound to the directory it captured on Windows, with the same refusals as other platforms: links and other reparse points are never followed, hard-linked outputs are refused, and Windows-special names are rejected. Windows does not honor symlink aliases of harness roots or symlinked user configuration files; it refuses them.
- Serialize concurrent ownership and dependency mutations on Windows with the same guarantees as on Unix.
- Default to file materialization mode on Windows when none is configured; report a stable diagnostic when an explicit symlink mode cannot create links.
- Acquire registry content without platform line-ending conversion, so locks, digests, and materialized bytes match across platforms.
- Keep committed acquisition locks byte-identical across platforms, accept CRLF checkouts without rewriting, and preserve line endings on configuration edits.
- Out of scope: the Marketplace service (`serve`) on Windows, Windows desktop GUI packages, a Windows install script, package-manager distribution (winget, Scoop), and Windows-specific hook command rewriting. Each needs its own change.

## Capabilities

### New Capabilities

- `platform-support`: supported platforms and Windows release binaries, Windows machine locations, Devin machine scope on Windows, the consumer organization configuration on Windows, permission requirements on Windows, filesystem boundaries on Windows, concurrent-mutation serialization, materialization mode on Windows, line-ending-neutral registry acquisition, and platform-independent committed files.

### Modified Capabilities

- `install-script`: the CLI release `SHA256SUMS` lists the Windows binaries in addition to the existing four; the POSIX install script continues to install only macOS and Linux binaries.

## Impact

Affects release build targets, the descriptor-bound filesystem layer (`internal/fsboundary` and the packages that open files with `openat` directly), ownership and dependency-transaction locking, process control, file identity and permission checks, organization trust validation, home and program-data resolution, materialization mode defaults, Git acquisition options, lock and configuration writers, and CI (a Windows build and test job). Uses `golang.org/x/sys/windows` from the existing direct `golang.org/x/sys` dependency. No consumer configuration, acquisition-lock, or API format change.

## Release note

agent-marketplace now runs on Windows: each release publishes `agent-marketplace-<tag>-windows-amd64.exe` and `agent-marketplace-<tag>-windows-arm64.exe`, verified by the release `SHA256SUMS`. On Windows, resources are installed as regular files by default; set `materialization.mode: symlink` only with Developer Mode or administrator rights. Devin machine-scope installation is not yet available on Windows.
