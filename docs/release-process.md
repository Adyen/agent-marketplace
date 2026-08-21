# Release process and release tag hardening

Release automation runs privileged work — it builds the published binaries and
writes a GitHub Release — using a value that comes from a pushed Git tag name.
Git accepts tag names containing shell and Make syntax, so **the tag name is
untrusted input** and is validated before anything else consumes it.

This document is the contract for that path: the accepted tag form, where it is
enforced, which permissions the workflow holds, and which repository settings
restrict who can start a release.

## Release tag contract

```
v<MAJOR>.<MINOR>.<PATCH>[-<prerelease>]
```

- Strict [SemVer 2.0.0](https://semver.org/) version core with a **mandatory
  leading `v`**: `v1.4.0`, `v0.2.11`, `v1.0.0-rc.1`, `v2.0.0-alpha.3`.
- **No build metadata** (`+...`). The tag is used verbatim in release artifact
  filenames (`agent-marketplace-v1.4.0-linux-amd64`) and in the workflow tag
  trigger glob, and `+` buys nothing in either place.
- Leading zeroes in numeric identifiers are rejected (`v01.2.3`, `v1.2.3-01`),
  as SemVer requires.
- Anything else is rejected: `latest`, `release/1.4.0`, the untagged-checkout
  `git describe` fallback (a bare commit hash such as `a58e897` or
  `a58e897-dirty`), and every tag carrying shell or Make syntax
  (`v1.0.0$(id)`, ``v1.0.0`id` ``, `v1.0.0'; id; '`, `$(shell id)`, a tag with an
  embedded newline).

  The gate checks form, not provenance, so `git describe` output is *not*
  categorically rejected: on an exact-tag checkout describe prints the tag
  name itself (`v1.4.0`), and `--long` output (`v1.4.0-2-gdeadbee`) is also
  strict SemVer shaped, so both pass. That is intended — the Makefile's
  describe fallback exists for `make build`, and release CI always passes the
  pushed tag name explicitly.

The single implementation of this contract is
[`scripts/validate-release-tag.sh`](../scripts/validate-release-tag.sh) (exit `0`
accepted, `1` rejected, `2` usage error). Nothing duplicates its regular
expression; every enforcement point calls it.

## Where the contract is enforced

These layers are defense-in-depth inside the repository; the non-rewritable
controls that restrict who can create or move a release tag are the GitHub
settings described further below.

| Layer | Enforcement |
|---|---|
| `on.push.tags` in `.github/workflows/ci.yml` | Only `v[0-9]+.[0-9]+.[0-9]+` and `v[0-9]+.[0-9]+.[0-9]+-*` start the workflow. GitHub filter patterns cannot express SemVer, so this is a coarse pre-filter, not the gate. |
| `release-tag-guard` job | Runs `scripts/validate-release-tag.sh` with `contents: read` and no persisted credentials. The tag is passed as the `RELEASE_TAG` environment variable and quoted — never spliced into a script through `${{ }}`. |
| `release` job | `needs: [release-tag-guard, ...]`, then re-validates the tag as its first step after checkout, so Make and the Go toolchain only ever see a validated value even if `needs` is later edited. |
| `Makefile` | `release` and every cross-compilation target depend on `check-release-tag`, which calls the same script. Artifacts cannot be produced from an unvalidated version. |
| `Makefile` variable handling | `VERSION` overrides are captured with `override VERSION := $(value VERSION)` so Make never expands an injected `$(shell ...)`, and recipes reference the exported `"$$VERSION"` instead of interpolating `$(VERSION)`, so quotes and semicolons cannot terminate a shell word. |

Regression tests for all of the above live in
[`internal/releaseguard`](../internal/releaseguard) and run as part of
`make test`. They execute the script and `make` as subprocesses with hostile tag
values and assert that no injected command runs, that no artifact is produced,
and that the workflow keeps its narrow trigger, guard ordering, and permission
model.

## Workflow permissions

The workflow declares `permissions: {}` at the top level, so every job opts in
explicitly:

| Job | Permissions | Why |
|---|---|---|
| `build`, `test`, `lint`, `govulncheck` | `contents: read` | Checkout only; all use `persist-credentials: false`. |
| `release-tag-guard` | `contents: read` | Must run before anything privileged, so it holds no write scope. |
| `release` | `contents: write` | Required solely to create the GitHub Release and upload its assets. |

No job requests `id-token`, `packages`, `actions`, or `pull-requests` write
scopes.

## Repository settings that restrict who can release

The controls that decide **who can start or alter a release** live in GitHub
repository settings, not in this repository. They are the non-rewritable part
of the model: a contributor who can push branches and open pull requests can
propose edits to the workflow, the Makefile, and the validator, but cannot
change these settings from a commit. Everything enforced in-repo — the trigger
globs, the `release-tag-guard` job, the in-job re-validation, the Makefile
guards — is **defense-in-depth** on top of them: it ensures that a tag which
exists anyway (misconfigured ruleset, a bypass credential, a compromised
release-manager account) still cannot smuggle shell or Make syntax into the
privileged job, and that the workflow's write scope is held only by a job that
runs after validation.

Both settings below live in GitHub, so they are recorded here and must be
re-checked when repository administration changes.

1. **Tag ruleset — `refs/tags/v*`**
   - _Restrict creations_: enabled, with the bypass list limited to the release
     managers (and, if used, the release automation app).
   - _Restrict updates_ and _Restrict deletions_: enabled, so a published
     release tag cannot be silently moved to different content.

   Effect: a contributor who can push a branch still cannot start a release, and
   cannot re-point an existing release tag.

2. **`release` environment**
   - _Required reviewers_: the release managers.
   - _Deployment branches and tags_: limited to the tag pattern `v*`.

   The `release` job declares `environment: release`, so publishing waits for an
   approval from that list even if a tag is somehow created outside the ruleset.

## Cutting a release

1. Make sure `main` is green.
2. Create and push the tag (release managers only):

   ```bash
   git tag -a v1.4.0 -m "v1.4.0"
   git push origin v1.4.0
   ```

3. CI runs `build`, `test`, `lint`, `govulncheck`, and `release-tag-guard`.
4. Approve the `release` environment deployment when prompted.
5. The `release` job re-validates the tag, runs `make release`, and uploads
   `dist/*` to the GitHub Release.

If the tag is rejected, the guard job fails with the reason and the expected
form, and no release job runs. Fix the tag name, delete the bad tag, and push a
correctly named one — the rejected value never reaches a build.

## Local release builds

`make build` is the day-to-day target and keeps using `git describe` output, so
untagged and dirty checkouts still build.

`make release` produces publishable, version-stamped artifacts and therefore
requires a valid release tag. On an untagged checkout, pass one explicitly:

```bash
make release VERSION=v0.0.0-dev
```
