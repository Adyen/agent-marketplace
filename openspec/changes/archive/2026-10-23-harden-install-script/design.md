## Context

`site/public/install.sh` is a bash script run through `curl | bash`. The tests in `scripts/install_script_test.go` run it offline with fake `curl`, `uname`, and (for failure cases) fake or removed hash and file tools. The first change left the install directory, the download order, and the messages for distinct failures implicit.

## Decisions

- **Local checks before network.** Install-directory resolution, the absolute-path check, and the directory-at-target check run before the latest-release lookup. A refused install then makes zero requests, which the tests assert from the fake `curl` log.
- **`~` expansion is explicit.** The default is a quoted literal `~/.local/bin` and expansion happens in the script, so an unset `HOME` is always caught by the `HOME is not set` check rather than by the shell's own tilde expansion.
- **One download helper.** `download <url> <out> <message-for-404>` maps curl exit failure to `could not reach`, 404 to the caller's message, and any other status to `download failed (HTTP n)`. The SHA256SUMS and binary downloads use it, so the three outcomes stay distinguishable.
- **Checksums first.** SHA256SUMS is fetched and its entry resolved before the binary is requested. A missing entry or missing file therefore never reaches the binary URL.
- **Stdin hashing.** `sha256sum < file` and `shasum -a 256 < file` print no file name, so the temporary directory path cannot change the digest comparison.
- **Shadow warning uses `command -v`.** It compares the resolved `agent-marketplace` with `<install_dir>/agent-marketplace` and warns on stderr. The PATH hint stays on stdout. The warning wording says "shadows" in both the earlier-on-PATH and not-on-PATH cases, since `command -v` reports whichever binary a shell would run.

## Risks

- The shadow comparison is textual, so a PATH entry that reaches the same file through a symlink or a trailing slash can produce a false warning. Accepted: the warning is advisory and the install is unaffected.
- The `mktemp -d` failure case uses a fake `mktemp` rather than a missing `TMPDIR`, because macOS `mktemp` falls back to the system temporary directory when `TMPDIR` is missing.
