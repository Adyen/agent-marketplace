# Design

## Context

`CODING_STANDARDS.md` lists Windows as a supported platform. The code compiles for `GOOS=windows` only through stubs, and several Unix assumptions would misbehave at runtime:

- `internal/ownership/filelock_other.go` returns `ErrLockUnsupported`, so ownership mutations degrade with `ownership.lock-unavailable`; `internal/dependencytxn` locks with `flock(2)`.
- Go reports Windows files as mode `0666` (or `0444`) and directories as `0777`. Organization trust (`perm&0o022`) and telemetry secret checks (`perm&0o077`) would therefore refuse every file, while `isTrustedOwner` on Windows always returns true.
- The organization configuration path is the constant `/etc/agent-marketplace/config.yaml`.
- Registry acquisition runs `git checkout` with system and global Git configuration disabled, but `core.eol` still defaults to the platform's native ending, so a registry declaring `* text=auto` checks out with CRLF on Windows.
- Materialization defaults to symlink mode, which unprivileged Windows users cannot use.

## Goals / Non-Goals

**Goals:**
- Ship Windows `amd64` and `arm64` CLI binaries with every stable release.
- Give every documented `~/...` location a single meaning on Windows, matching where each harness reads its machine configuration.
- Keep the Unix safety guarantees on Windows: serialized mutations, fail-closed ownership and organization trust, owner-only private files, no partial materialization.
- Keep committed repository files and registry digests identical across platforms so mixed-OS teams share one lock.
- Work out of the box for a standard, unprivileged Windows user.

**Non-Goals:**
- The Marketplace service (`serve`) on Windows; it remains container-deployed on Linux.
- Desktop GUI packages, a PowerShell install script, and package-manager manifests.
- Rewriting hook commands for Windows shells; `commandWindows` and native fields keep their current pass-through behavior.
- Network-filesystem locking guarantees beyond those already defined for Unix.
- Supporting Windows versions the Go toolchain itself does not support (Windows 10 / Server 2016 and later are supported by current Go).

## Decisions

### Home directory and harness machine roots

`~` means the profile directory Windows reports for the user (`%USERPROFILE%`), which is what Go's `os.UserHomeDir` returns and what Node (`os.homedir`) and Rust-based harnesses use. `HOME`, which Git Bash and MSYS2 set, is ignored, so a command behaves the same from PowerShell, cmd, and Git Bash. Claude Code, Codex, Cursor, Factory, Gemini CLI, OpenCode, and Windsurf use profile-relative dot directories on Windows, so their machine roots keep the same relative paths and existing specs stay valid.

Alternative considered: `%APPDATA%` / `%LOCALAPPDATA%` for Agent Marketplace's own files. Rejected: it would split one product across two conventions and require per-platform rewrites of existing scenarios, with no user benefit.

Devin is the exception. Its Unix machine root is `~/.config/devin`, and its Windows location has not been verified (the Devin harness plan already lists `%APPDATA%\devin` as an unhandled case). Writing to a guessed location would silently install resources Devin never reads. Machine-scope Devin output on Windows is therefore skipped with the warning `harness.devin.windows-machine-scope-unsupported`, following the existing pattern of `rule.devin.unsupported`. Repository-scope Devin output (`<repo>/.devin`) is unaffected. A follow-up change enables it once the root is confirmed.

### Consumer organization configuration and trust

The configuration and lock live in `agent-marketplace\` under the program-data known folder (`FOLDERID_ProgramData`, by default `C:\ProgramData`). The location is resolved through the known-folder API, not the `ProgramData` environment variable: on Unix the path is a constant a user cannot redirect, and reading an environment variable would let any user bypass organization policy by setting it.

Trust differs from Unix by necessity. `/etc` is writable only by root, so on Unix a wrong-owner organization configuration is reported as a warning. The program-data folder lets ordinary users create subfolders by default, so any local user could create `C:\ProgramData\agent-marketplace\config.yaml` and impose policy on every other user of the machine. On Windows the `agent-marketplace` directory, the configuration, its includes, and its lock must therefore be owned by SYSTEM, Administrators, or TrustedInstaller and grant write, delete, change-permission, or take-ownership access to no other principal. Otherwise the configuration is refused with the existing `organization.trust.*` codes. "Owner is root" maps to these trusted principals and "group/other writable" maps to an ACL check, replacing the mode-bit checks on Windows.

### Permissions

Owner-only requirements (backup archives, telemetry state and credentials, private state directories) map to an access control list granting only the current user, SYSTEM, and Administrators. Files created under `%USERPROFILE%` inherit that ACL by default; writers set it explicitly so files created elsewhere (for example under an `XDG_STATE_HOME` override) are also private. Permission checks read the ACL instead of POSIX mode bits on Windows.

### Locking

Ownership and dependency-transaction locks use `LockFileEx` on an exclusive byte range of the existing lock files, through `golang.org/x/sys/windows`. Windows releases the lock when the handle closes or the process exits, matching the crash semantics the Unix `flock` design relies on. `golang.org/x/sys` is already a direct dependency, so no module is added.

### Materialization mode

On Windows the default materialization mode is file when no layer sets one. Symlinks need Developer Mode or `SeCreateSymbolicLinkPrivilege`, which most corporate machines do not grant, so a symlink default would make the first sync fail for most users. The materialization manifest and ownership state are local and never committed, so a per-platform default does not affect teammates on other platforms.

An explicit `materialization.mode: symlink` is honored. When link creation is refused, sync reports `materialize.symlink.unavailable` for that harness target and writes nothing for it. It does not fall back to copies: a silent mode switch would change ownership and pruning behavior without the user choosing it. `sync --check` must not create links to probe, so it reports missing outputs as ordinary drift.

Alternative considered: directory junctions, which need no privilege. Rejected: subagents are file links and junctions cannot link files; Go reports junctions as irregular files rather than symlinks, which would change managed-link classification and pruning; and mixing link types per resource kind would complicate ownership. Junctions may be reconsidered as a separate change.

### Line endings

Registry acquisition passes `-c core.autocrlf=false -c core.eol=lf` to every Git command that writes a working tree. Explicit `eol=` attributes in a registry still apply, and apply identically on every platform. Git acquisition also passes `-c core.longpaths=true` so deep registry trees check out under the cache.

Acquisition locks are always written with `\n` and `/`. A Windows checkout with `core.autocrlf=true` produces CRLF lock files; comparing them after normalizing line endings avoids rewriting a healthy committed lock and keeps the existing "lock bytes stay identical through plain sync" guarantee. Configuration edits preserve the edited file's line-ending style to avoid whole-file diffs. New files use `\n`.

### Filesystem safety layer

The Unix guarantees ("descriptor-based filesystem safety" in `CODING_STANDARDS.md`) rest on `openat`/`renameat`/`unlinkat` against captured directory descriptors with `O_NOFOLLOW`. They live in `internal/fsboundary` and are re-implemented ad hoc, with raw `unix.Openat`, in about thirteen more files (`internal/materialize` hook and support capture, `internal/dependencytxn`, `internal/lockfile`, `internal/cache`, `internal/updatepreview`, `internal/marketplace`, `pkg/harness-compilation/stale_prune.go`). On Windows `syscall.O_NOFOLLOW` and `O_NONBLOCK` compile but are ignored by the operating system, so code using them would build and silently lose its no-follow protection. "Compiles on Windows" is therefore not the acceptance criterion for this layer; each site needs a Windows implementation that is tested to refuse links.

Decision: implement a Windows twin of `fsboundary` with the same API and the same refusal identities (`ErrUnsafePath`, `ErrUnsafeFile`, `ErrGenerationConflict`, `ErrTooLarge`), then move the ad-hoc sites onto it. This follows Go's own `os.Root` implementation for Windows (`internal/syscall/windows/at_windows.go`), which is the reference for the native calls:

- A captured `Dir` holds a directory handle. Every open is `NtCreateFile` with `OBJECT_ATTRIBUTES.RootDirectory` set to that handle and a single path component as the name, so a path renamed or replaced after capture cannot redirect it. This is the Windows equivalent of `openat`.
- Walking one component per call with `FILE_OPEN_REPARSE_POINT` opens a reparse point itself instead of following it. The opened handle is then inspected (`FILE_ATTRIBUTE_REPARSE_POINT`) and refused as `ErrUnsafePath`. This is the equivalent of `O_NOFOLLOW`, and it covers junctions, mount points, and every other reparse tag, not only symlinks. `OBJ_DONT_REPARSE` is not used alone: Go falls back to the same per-component method on Windows 10 build 10240, and the per-component form works on every supported release.
- Object identity comes from the handle: volume serial number and the 128-bit file ID (`FileIdInfo`), plus the hard-link count and change time from `FILE_BASIC_INFO` and `GetFileInformationByHandle`. Leaf opens refuse a link count other than one, matching the Unix contract.
- Create, mkdir, rename, and unlink are handle-relative: `NtCreateFile` with `FILE_CREATE`, `FILE_DIRECTORY_FILE`, and rename and delete by `NtSetInformationFile` on a handle opened relative to the captured directory. Deletion uses POSIX semantics where the file system supports them, so an open handle does not block removal.
- Windows has no portable directory flush. `Dir.Sync` flushes the handle when the file system allows it and otherwise succeeds without flushing; NTFS journals metadata. `ErrDurabilityUncertain` is not reported on Windows for a directory sync that the platform cannot perform.

Name rules are stricter on Windows. A leaf or component is refused when it is a reserved device name (with or without an extension), contains `:` (alternate data stream), ends in a dot or a space, or contains a character Windows forbids. `NtCreateFile` relative to a root handle does not apply DOS device-name resolution, so these names would otherwise be creatable but unreachable by every other Windows tool.

Limitations accepted for the first Windows release, all fail-closed:

- Symlink aliases of harness roots, skill support directories, and symlinked user configuration files are refused as unsafe links. The Unix link policies (`LinkPolicyUser`, honored directory-root aliases) need a reparse-data reader and per-target ownership checks; they can be added later without weakening the default.
- `FileInfoIdentity(os.FileInfo)` cannot return a file ID, because a Windows `os.FileInfo` does not carry one. About thirty files compare identities obtained from `os.Root.Lstat` or `File.Stat`. Until they move to handle-derived identity (the next implementation stage), the Windows `FileInfoIdentity` compares creation time, size, modification time, and mode, and reports a link count of one because it cannot see the real count. It therefore cannot detect a hard link, and its result is not comparable with an identity taken from a handle. Hard-link refusal on Windows is enforced at handle open, not at `FileInfoIdentity`. The stage that migrates those call sites also removes this weaker path.
- `Dir.NativeRoot` (an `*os.Root` for callers that use the standard library API) opens the directory by its final path name and then verifies that the opened directory has the captured identity, because `os.Root` cannot be constructed from a handle. A directory replaced between the two steps is detected and refused as `ErrGenerationConflict`; it is not prevented.

In scope after `fsboundary`: `internal/ownership`, `internal/dependencytxn`, `internal/lockfile`, `internal/cache`, `internal/materialize`, `internal/updatepreview`, `internal/marketplace`, `pkg/harness-compilation/stale_prune.go`, and the `O_NONBLOCK`/`O_NOFOLLOW` uses in `internal/config`, `internal/pipeline`, `internal/session`, and `pkg/marketplace/config`. Each is converted to a shared primitive (not another per-package copy) and covered by a test that fails if the link is followed.

### Build tooling and CI

The Makefile stays the single build definition. On Windows it selects the `sh.exe` that Git for Windows ships, because every recipe is POSIX shell and GNU make otherwise uses `cmd.exe`; it fails with a clear message when none is on `PATH`. It also writes `SHA256SUMS` entries as bare names, because Git's `sha256sum` marks entries as binary. The Windows release targets (`windows-amd64`, `windows-arm64`) exist but join the default `release` only when `WINDOWS_RELEASE` is set, until the CLI compiles for Windows; otherwise a failing prerequisite would stop `make release` before it writes the macOS and Linux assets. These are conveniences for local use. CI does not depend on them: the Windows job calls `go vet` and `go test` directly (the existing `make test-witnessed` is `go test -json ./... | go run ./tools/witness-check`), so it works regardless of how a contributor's shell is configured.

Contributors build and run Windows-specific tests natively, because Windows behavior can only be verified on Windows, and run the full gates (`make validate`, `make spec-check`, `specaudit gate`, `make e2e`, shellcheck, TUI QA) in WSL from a clone in the WSL file system. The gates are Linux-oriented and CI runs them on Linux. A repository under `/mnt/c` has different timestamp behavior and makes timestamp-sensitive tests fail on an unchanged tree.

Rejected for this change:

- A parallel Windows build file (`build.ps1` or a second Makefile). Two build definitions drift.
- Replacing Make with Mage, Task, or `just`. It would not remove the larger Windows costs (the filesystem layer, POSIX-only tests, Linux-only gates), it rewrites about 570 lines of recipes, and every reference in `AGENTS.md`, CI, and the docs changes. It is a separate decision for the maintainers. The repository already moves logic into Go tools (`tools/specaudit`, `tools/test-local`, `tools/witness-check`); continuing that, with `make` as a thin wrapper, gives portability gradually.

The CI gate treats a skipped required witness as a failure, so a Windows run needs its own required-witness list and Unix-only tests tagged or replaced; tagging alone would trip the gate. Windows E2E runs without Docker, and TUI QA stays Linux-only until a Windows console approach exists.

### Code organization

Platform-specific behavior lives in `_unix.go` / `_windows.go` files per concern (locking, process control, file identity, permissions and ACLs, open flags, program-data resolution), each with one authoritative policy, as `CODING_STANDARDS.md` requires.

## Risks / Trade-offs

- **Stricter organization trust on Windows** than the Unix warning. Deployers must set ACLs correctly; misconfiguration refuses configuration-dependent commands. This is intentional, because the Windows default ACL is weaker than `/etc`.
- **Safety layer correctness.** The Windows twin of `fsboundary` is security-critical code that cannot be proved by compilation. Every refusal (reparse point, hard link, replaced directory, special name) needs a Windows test that fails if the protection is removed, and the work needs a security review before release.
- **TUI QA** journeys run in a Unix pseudo-terminal and do not cover the Windows console. Windows-specific TUI code (editor process signals) still needs a journey under the `AGENTS.md` rule; Windows console coverage is a follow-up.
- **Git for Windows** must be installed. The cache passes `/dev/null` as a config and hooks path; Git for Windows maps it to `NUL`, and this must be verified in the Windows CI job.
- **Symlink creation** on Windows decides file or directory link type from the target at creation time. Staged links must be created where the relative target resolves.
- **Case-insensitive names** on NTFS can merge resources that differ only in case; existing case-collision diagnostics must be exercised on Windows. Windows-reserved names (`CON`, `NUL`) in Git registries need a coded diagnostic rather than a checkout failure.
- **Antivirus and indexers** can briefly hold files open, making rename-over fail. Publication must surface a coded error rather than retry indefinitely.
- **Scope.** This change is broad. If review prefers, it can be split into release/compile, locations and trust, locking, and materialization changes without changing the requirements.

## Open Questions

- Confirm the program-data location and the Windows trust rule with deployers who manage Windows fleets.
- Confirm Devin's Windows machine root so the Devin exception can be lifted.
- Decide whether Windows `arm64` ships in the first release or after a native runner is available for testing.
