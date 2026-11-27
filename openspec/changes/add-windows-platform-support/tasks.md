# Tasks

## 1. Decisions

- [ ] 1.1 Confirm the open questions in `design.md` with the maintainers: the program-data location and Windows trust rule, Devin's Windows machine root, and `arm64` timing. Update the spec deltas if any answer changes.

## 2. Build and release

- [x] 2.1 Add `windows-amd64` and `windows-arm64` targets producing `.exe` assets, listed in the CLI release `SHA256SUMS` when `WINDOWS_RELEASE` is set. They stay out of the default `release` until the CLI compiles on Windows, because a failing prerequisite would stop `make release` before it writes the macOS and Linux assets.
- [x] 2.2 Extend `scripts/build_release_test.go` with a test annotated `// spec: install-script/release-build-writes-checksums-for-the-windows-binaries` asserting exactly six entries; keep the existing four-binary annotation.
- [x] 2.3 Add a test annotated `// spec: platform-support/release-build-produces-windows-binaries`.
- [ ] 2.4 Add a Windows CI test annotated `// spec: platform-support/windows-binary-reports-its-version` that runs the Windows build with `--version`.

- [x] 2.5 Make the Makefile run on Windows: select Git for Windows' `sh.exe` (the recipes are POSIX shell, and GNU make otherwise uses `cmd.exe`), fail with a clear message when no POSIX shell is on `PATH`, and write `SHA256SUMS` entries as bare names because Git's `sha256sum` marks entries as binary (`hash *name`). Linux output is unchanged. The targets themselves (`build`, `test`) still depend on the Windows compile work below.
- [ ] 2.6 Make the Windows release assets part of the default `release` (drop the `WINDOWS_RELEASE` opt-in) once `GOOS=windows go build ./cmd/agent-marketplace` succeeds, and run the release workflow once to confirm all six assets and `SHA256SUMS`.

## 3. Filesystem safety layer and Windows compilation

Stage 1: `internal/fsboundary` on Windows.

- [x] 3.1 Implement the Windows twin of `internal/fsboundary` with handle-relative `NtCreateFile` opens (one component at a time, `FILE_OPEN_REPARSE_POINT`, reparse points refused as `ErrUnsafePath`), handle-derived identity (volume serial, file ID, link count, change time), singly-linked leaf validation, bounded reads, staging, create/mkdir/rmdir/unlink, destination capture, verification, publication, and leaf removal, preserving the existing refusal identities. Reject reserved device names, `:` and names ending in a dot or space.
- [x] 3.2 Add Windows tests for every refusal: symlink, directory junction, mount point, and replaced-after-capture directory as an intermediate component and as a leaf; hard-linked leaf; reserved and stream names; directory renamed after capture; bounded-read growth; publication over a replaced destination. Each must fail if the protection is removed. Done in `internal/fsboundary/boundary_windows_test.go`, with each protection mutation-checked. The file-symlink test needs Developer Mode or administrator rights and is skipped on hosts without them; an unhandled-tag reparse point covers the same open path without privilege.
- [ ] 3.3 Add command-level tests annotated `// spec: platform-support/directory-junction-below-a-harness-root-is-refused`, `// spec: platform-support/hard-linked-output-is-refused`, `// spec: platform-support/windows-special-names-are-refused`, and `// spec: platform-support/symlinked-user-configuration-is-refused`.
- [x] 3.4 Update the `internal/fsboundary` package documentation, which currently states macOS and Linux only.

Stage 2: move ad-hoc descriptor code onto shared primitives.

- [ ] 3.5 Replace per-package `unix.Openat`/`O_NOFOLLOW` code with `fsboundary` primitives in `internal/materialize` (hook support, source fingerprint, native hook capture and forwarding), `internal/dependencytxn`, `internal/lockfile`, `internal/cache`, `internal/updatepreview`, `internal/marketplace`, and `pkg/harness-compilation/stale_prune.go`. Unix behavior and test results stay unchanged.
- [ ] 3.6 Replace the `O_NONBLOCK`/`O_NOFOLLOW`/`SetNonblock` uses in `internal/config`, `internal/pipeline`, `internal/session`, and `pkg/marketplace/config` (they compile on Windows but do nothing there) with a shared bounded no-follow regular-file open.
- [ ] 3.7 Move callers of `FileInfoIdentity` on `os.Root.Lstat`/`File.Stat` results to handle-derived identity on Windows, then remove the size/time-only Windows `FileInfoIdentity`.

Stage 3: remaining platform code and CI.

- [ ] 3.8 Make `CGO_ENABLED=0 GOOS=windows go vet ./...` pass for the root module, moving remaining Unix-only code (`syscall.Stat_t`, process groups, `SIGTERM`, `os.Getuid`, `Mkfifo`) behind `_unix.go` / `_windows.go` files per concern.
- [ ] 3.9 Add a Windows CI job (`windows-latest`, `shell: bash`) that calls `go` directly, not `make`: `go vet ./...` and `go test -json ./... | go run ./tools/witness-check`. Verify Git for Windows accepts the `/dev/null` config and hooks paths used by the cache.
- [ ] 3.10 Make the witness gate platform-aware: a Windows list of required witnesses, and Unix-only tests tagged or replaced by Windows equivalents, so that no required witness is skipped on either platform. A skipped required witness must still fail the gate.
- [ ] 3.11 Run Windows E2E in process mode (`E2E_SKIP_DOCKER=1`; Linux containers are unavailable on Windows runners). Keep TUI QA Linux-only until a Windows console story exists, and record that as a known gap.
- [ ] 3.12 Add the Windows job as a non-required check first, then require it, through an aggregate status like `E2E Gates`, once it is stable.
- [ ] 3.13 Extend the command-test sandbox to isolate `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, and the program-data location.
- [ ] 3.14 Obtain a security review of the Windows filesystem layer before the first Windows release.

## 4. Machine locations

- [ ] 4.1 Resolve `~` from the Windows profile directory, ignoring `HOME`, in one authoritative place.
- [ ] 4.2 Add command tests annotated `// spec: platform-support/user-layer-resolves-under-the-profile-directory` and `// spec: platform-support/home-does-not-relocate-machine-locations`.
- [ ] 4.3 Skip machine-scope Devin output on Windows with `harness.devin.windows-machine-scope-unsupported`, registered in the diagnostic catalog; add a test annotated `// spec: platform-support/machine-scope-devin-selection-is-skipped-with-a-warning`.

## 5. Organization configuration and permissions

- [ ] 5.1 Resolve the organization configuration and lock under the program-data known folder on Windows, not the `ProgramData` environment variable.
- [ ] 5.2 Replace mode-bit and uid trust checks on Windows with ACL-based owner and write-access checks, keeping the existing `organization.trust.*` codes, and make the Windows organization configuration trust failure refuse rather than warn.
- [ ] 5.3 Add tests annotated `// spec: platform-support/organization-configuration-is-read-from-the-program-data-folder`, `// spec: platform-support/programdata-environment-variable-does-not-redirect-the-organization-layer`, and `// spec: platform-support/organization-directory-created-by-an-ordinary-user-is-refused`.
- [ ] 5.4 Create owner-only files and directories on Windows with an ACL granting only the current user, SYSTEM, and Administrators, and check private files by ACL. Add tests annotated `// spec: platform-support/private-file-created-on-windows-is-owner-only` and `// spec: platform-support/owner-only-file-under-the-profile-directory-is-accepted`.

## 6. Locking

- [ ] 6.1 Implement ownership and dependency-transaction locks with `LockFileEx` on Windows.
- [ ] 6.2 Add tests annotated `// spec: platform-support/concurrent-installs-into-one-repository-keep-both-selections` and `// spec: platform-support/a-terminated-holder-releases-its-lock`, running on the Windows CI job.

## 7. Materialization mode

- [ ] 7.1 Default the materialization mode to file on Windows when no layer sets one, in the single place defaults are resolved.
- [ ] 7.2 Detect refused symlink creation on Windows and report `materialize.symlink.unavailable` per harness target before writing its outputs; register the code in the diagnostic catalog. Ensure `sync --check` never creates probe links.
- [ ] 7.3 Add tests annotated `// spec: platform-support/default-materialization-mode-on-windows-is-file` and `// spec: platform-support/symlink-refusal-is-reported-without-partial-output`.

## 8. Line endings and committed files

- [ ] 8.1 Pass `core.autocrlf=false`, `core.eol=lf`, and `core.longpaths=true` to every Git command that writes a registry working tree, in both the consumer cache and Marketplace acquisition.
- [ ] 8.2 Add a test annotated `// spec: platform-support/registry-with-text-auto-detection-verifies-on-windows`.
- [ ] 8.3 Treat CRLF-only lock differences as current; preserve line endings on configuration edits; write `/`-separated relative paths.
- [ ] 8.4 Add tests annotated `// spec: platform-support/lock-written-on-windows-matches-linux`, `// spec: platform-support/lock-checked-out-with-crlf-line-endings-is-current`, and `// spec: platform-support/configuration-edit-preserves-crlf-line-endings`, using a committed Linux-written lock fixture.

- [ ] 8.5 Add a `.gitattributes` that pins LF for shell scripts, the Makefile, and other files executed by a POSIX shell. The repository has none, so a default Windows checkout (`core.autocrlf=true`) turns `set -euo pipefail` into `pipefail\r` and breaks every script and Makefile recipe under WSL or Git Bash. Add a test that fails when a tracked shell script has CRLF line endings in the index.

## 9. Documentation

- [ ] 9.1 Update `site/docs/install.md` with Windows download and verification steps, the Git for Windows prerequisite, the file materialization default, the symlink privilege note, and the Devin machine-scope limitation.
- [ ] 9.2 Document the Windows organization configuration location and required ACLs for deployers.

- [ ] 9.3 Document the contributor workflow for Windows: build and run Windows-specific tests natively; run the full `make` gates (`make validate`, `make spec-check`, `specaudit gate`, `make e2e`) in WSL from a clone inside the WSL file system, not under `/mnt/c`; rely on CI for both platforms. Note that `make` on Windows needs Git Bash or Git's `usr\bin` on `PATH`.

## 10. Verification and archive

- [ ] 10.1 Run `make spec-check`, `make validate`, and the Windows CI job; record each result.
- [ ] 10.2 Run `openspec archive add-windows-platform-support --yes` as the final commit of the pull request.
