## Context

See [proposal.md](proposal.md) for motivation and scope. The existing stable-tag classifier accepts only `vMAJOR.MINOR.PATCH`; the `release` job waits for the current release gates, runs `make release`, and uploads the four raw files produced by the Makefile (`darwin-arm64`, `darwin-amd64`, `linux-arm64`, and `linux-amd64`). There is no existing Homebrew packaging or tap workflow. The new publisher must consume the completed release without changing those artifacts or gates.

## Goals / Non-Goals

**Goals:**
- Generate reproducible formula and tap version files from a stable release's exact binary bytes and uploaded asset identities.
- Support both anonymous downloads after the repository is public and authorized downloads while it is private, without persisting credentials.
- Publish through GitHub's API to a dedicated, scoped, monotonically updated `homebrew` branch.

**Non-Goals:**
- Create or depend on a separate tap repository, add release binaries or alter release gates, or modify the source branch.
- Provide anonymous worldwide access while the repository and its release assets remain private.
- Change organization-managed Homebrew policy or add user-facing README/site documentation.

## Decisions

### Generate from exact release inputs, without generator network access

Add a focused Go tool under `tools/homebrew`. Its generator takes the exact stable tag, validated owner/repository, the four local binary paths, uploaded release metadata, and the repository visibility mode. The release job has already uploaded the binaries; the publication job obtains the release JSON and downloads those exact named assets for local hashing. Before writing any output, generation validates the stable tag, repository coordinate, four matching unique asset names, positive numeric asset IDs, and readable binaries. It computes SHA-256 from the bytes it received, renders all outputs deterministically, and writes them only after validation. It does not make network requests; the workflow owns release discovery and download.

The formula is `AgentMarketplace` in `Formula/agent-marketplace.rb`, with Homebrew's version metadata set to the release version without the `v` prefix. It maps macOS and Linux arm64/amd64 to the corresponding raw binary and fails rather than choosing a fallback for another platform. Installation renames the selected binary to `bin/agent-marketplace`; the formula declares Apache-2.0 and Git, and its test asserts that `--version` returns the exact stable tag including `v`.

### Keep public and private download paths distinct and credential-safe

For a public repository, generated platform blocks use canonical GitHub release download URLs and `using: :nounzip`. For a private repository, each formula block uses the immutable numeric asset ID with the GitHub release-assets API and binary-content Accept header. A `NoUnzipCurlDownloadStrategy` subclass reads `HOMEBREW_AGENT_MARKETPLACE_TOKEN` only when fetching; it rejects an unset token before the request and adds authorization headers temporarily, removing them in cleanup. The formula source and generated metadata never contain the token. Normal curl redirect behavior is retained: authorization is not forwarded to the redirected asset host. Do not use `--location-trusted` or another mechanism that forwards credentials across hosts.

The release workflow supplies visibility as a non-secret generator input. A formula generated after the repository becomes public uses the anonymous canonical URL path. While the repository is private, installation is limited to people with authorized source/tap access and a suitable token provided securely in their environment at fetch time.

### Publish in a separate, serialized stable-release job

Add a publication job that depends on both successful `release` and the exact stable classification. It runs after the existing release job has completed all gates and uploaded `dist/*`; the new job declares `contents: write` while the workflow-level `permissions: {}` remains unchanged. Preserve the existing release job's separate `contents: write` permission unchanged. The publisher has a fixed repository tap concurrency group with `cancel-in-progress: false`; it uses `gh api` and the job's `GITHUB_TOKEN` and does not check out or push a branch from the release worktree.

The publisher owns only `Formula/agent-marketplace.rb`, an optional formula download-strategy support file, and `VERSION` on `homebrew`. If that branch does not exist, it creates the initial tree and ref. Otherwise, it reads the existing ref/tree, creates a tree based on that tree, and commits with the existing commit as parent. It updates only the owned paths, preserving unrelated tree entries and history. The ref update uses `force: false`; a race or API error fails the job without moving the ref. No write targets the source branch, and the formula or helper is not added to the release assets.

`VERSION` stores canonical `MAJOR.MINOR.PATCH` without `v`, allowing strict numeric comparison with the incoming tag. An older release is skipped; the same version with byte-identical owned outputs is a no-op; malformed version or branch metadata fails closed. As a conservative immutable-release rule, the same version with differing owned output is treated as a conflict and is not rewritten.

### Test the generated contract and the publication boundary

Go tests cover stable-tag and repository validation, deterministic output, all four checksums and asset matches, invalid-input no-output behavior, and the publisher's API boundary. A fake `gh` executable or local Go HTTP server exercises first branch creation, normal update, preservation of unrelated files, idempotence, older-version skips, stale-ref rejection, and API failures. Formula contract tests evaluate the generated Ruby against a lightweight Homebrew DSL stub to exercise all OS/architecture branches, binary renaming, version behavior, and private authentication deferred to fetch; they must not require a host-managed Homebrew installation. CI may add a real Homebrew smoke where a suitable runner is available, but it is not a substitute for the deterministic local contract tests.

Workflow structural tests assert stable-tag gating, dependency on successful release upload, that the new publisher job declares `contents: write`, the workflow-level `permissions: {}` remains unchanged, and the existing release job's permission is preserved, along with tap concurrency and no source-branch writes. Each scenario in `specs/homebrew-packaging/spec.md` must be bound to a test with its exact `// spec: homebrew-packaging/<scenario-slug>` annotation.

## Risks / Trade-offs

- [Private installs require both tap/source access and a release-read token] → Explain the separate access requirements; obtain the token through the user's approved secret mechanism and read it only at fetch time. Private releases cannot be made anonymously available by this tap.
- [GitHub's private-asset API redirect behavior may change] → Authenticate only the API request, retain curl's safe cross-host redirect handling, test request headers and token redaction, and fail closed when the required token is absent.
- [Concurrent releases can race despite serialized workflow dispatch] → The publisher compares numeric versions and updates refs without force; stale refs and API conflicts fail visibly rather than overwriting newer or unrelated work.
- [An organization-managed Homebrew wrapper may reject custom taps or repository inspection] → Document that behavior as host policy; do not bypass the wrapper or claim the tap is usable where that policy disallows it.

## Migration Plan

The first successful stable publication creates `homebrew`; subsequent publications extend it. Existing release installations and assets remain usable throughout. Internal users install with an authorized tap checkout and securely provision `HOMEBREW_AGENT_MARKETPLACE_TOKEN` in the environment used for Homebrew's asset fetch; no token is placed on the command line or in tracked files. Tap setup is:

```sh
brew tap --custom-remote adyen/agent-marketplace git@github.com:Adyen/agent-marketplace.git
git -C "$(brew --repository adyen/agent-marketplace)" fetch origin homebrew
git -C "$(brew --repository adyen/agent-marketplace)" checkout --track origin/homebrew
brew install agent-marketplace
```

Do not use `brew tap --branch`. If a release formula is defective, publish a corrected forward stable release so the version remains monotonic; do not silently rewrite the tap to an older release. When the repository becomes public, future formula generation switches to anonymous canonical release URLs, and consumers can use `https://github.com/Adyen/agent-marketplace.git` as the custom tap remote for anonymous branch access. No anonymous access is promised while it is private.
