## Context

Release binaries are built by `make release` into `dist/` and uploaded with `dist/*` by the release job; Homebrew publication then downloads `agent-marketplace-$TAG-*`. The public site is a VitePress build whose `site/public/` directory is copied verbatim to the output root, and its container build context is `site/` only.

## Goals / Non-Goals

**Goals:** one copyable command, verified downloads, no `sudo`, no GitHub API rate limits.

**Non-Goals:** Windows, authenticated or `gh`-based downloads, site hosting changes, checksum backfill for earlier releases, Homebrew changes.

## Decisions

- **Script lives in `site/public/install.sh`.** VitePress publishes it unchanged at `<base>install.sh`, and the site Docker build (context `site/`) includes it. A repo-root copy or symlink would either drift or break the site build context. Alternative: repo-root script copied at build time — rejected for the build-context reason.
- **Checksum asset named `SHA256SUMS`.** It does not match `agent-marketplace-$TAG-*`, so the Homebrew download step keeps fetching exactly the four binaries. `make release` writes it after the four platform builds with bare file names so `sha256sum --check` works in `dist/`.
- **Latest tag from the redirect.** `curl -fsSLI -o /dev/null -w '%{url_effective}'` on `/releases/latest` yields `/releases/tag/<tag>` without API rate limits; the tag is validated against the stable pattern.
- **Verify then atomic install.** Download into `mktemp -d` (removed by an EXIT trap), verify, copy into a temp file in the install directory, `chmod 0755`, then `mv -f` over the target. Failure before the final `mv` leaves the install directory untouched.
- **Testability.** Tests run the script with a PATH holding fake `curl` and `uname` that serve fixture files and record requests, so every scenario runs offline. Downloads use `curl -fsSL --proto '=https'`.

## Risks / Trade-offs

- [Existing releases lack `SHA256SUMS`] → the script fails closed with a message naming the missing checksum file; the install guide keeps the manual download path.
- [Repository is internal today] → anonymous downloads return 404 until the public launch; the error names the failed URL. The landing page already targets the public launch.
- [`curl | bash` runs remote code] → the script is short, served from the project origin over HTTPS, and verifies the binary it installs; the guide shows how to download and inspect it first.
