# How to validate a registry in CI

Make a registry's CI job fail when a resource it ships is malformed, missing from discovery, or cannot be compiled for the harnesses your consumers use.

## Prerequisites

- A CI runner with Git, plus either network access to GitHub releases or Go to build from source.
- A checkout of the registry repository.
- The registry's source layout (`claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, or `codex`).

## 1. Install a pinned CLI

Pin the version so CI results are reproducible and match what consumers run. Use v4.0.9 or newer, because consumers need at least that release to read the lockfiles current releases write (v4.0.8 rejects them). Download the release asset for that version:

```bash
# Download a pinned release binary (Linux runner example)
VERSION="v4.0.9"
ARCH="linux-amd64" # or linux-arm64, darwin-arm64, darwin-amd64
curl -fsSL "https://github.com/Adyen/agent-marketplace/releases/download/${VERSION}/agent-marketplace-${VERSION}-${ARCH}" -o /usr/local/bin/agent-marketplace
chmod +x /usr/local/bin/agent-marketplace
agent-marketplace --version
```

The last command prints `v4.0.9`. If writing to `/usr/local/bin` fails with `permission denied`, put `sudo` in front of the `curl` and `chmod` commands, or download into a directory on the runner's `PATH` that the job owns. There is no Windows build.

To build from source at the pinned tag instead:

```bash
git clone --depth 1 --branch v4.0.9 https://github.com/Adyen/agent-marketplace.git /tmp/agent-marketplace
(cd /tmp/agent-marketplace && CGO_ENABLED=0 go build -ldflags "-X main.version=v4.0.9" -o /usr/local/bin/agent-marketplace ./cmd/agent-marketplace)
rm -rf /tmp/agent-marketplace
```

The `-ldflags` value makes `agent-marketplace --version` print the tag. Without it, the binary prints a Go pseudo-version.

Avoid unpinned builds or `@latest` in CI. A new release can change diagnostics and make an unchanged registry fail.

## 2. Point a throwaway consumer at the checkout

```bash
REGISTRY_PATH=$PWD
CONSUMER=$(mktemp -d)
mkdir -p "$CONSUMER/.agents"
cat > "$CONSUMER/.agents/config.yaml" << EOF
apiVersion: agent-marketplace/v4
harnesses:
  - claude
  - factory
  - cursor
  - windsurf
  - gemini
  - devin
  - codex
sources:
  checkout:
    directory: ${REGISTRY_PATH}
resources:
  checkout:
    all: true
    required: true
EOF
```

- List every harness your consumers target. A resource that compiles for Claude Code can still produce an error for Codex or a warning for Windsurf.
- A `directory` source reads the checkout directly and creates no lockfile, which is what you want in CI.
- `all: true` selects every resource of every kind. `required: true` stops an exclusion in another configuration layer on the runner from hiding a resource.

## 3. Sync strictly and check the summary

```bash
set -o pipefail
cd "$CONSUMER"
agent-marketplace sync --strict -v | tee sync.log
grep -q 'Malformed 0  Errors' sync.log || { echo "registry has malformed resources" >&2; exit 1; }
agent-marketplace sync --check
```

- `sync` exits with status `1` when any resource fails to compile for a selected harness (`Errors` greater than `0`). Warnings, such as a subagent that Windsurf cannot represent, do not fail the run.
- `--strict` makes sync exit with status `1` when a resource named in a configuration or a collection is missing or fails to load. `all: true` also selects every collection, so this checks that every collection member exists.
- With `all: true`, a resource that fails to load is left out of the catalog and counted under `Malformed` without failing the run, even with `--strict`. The `grep` line catches it. `-v` lists each malformed resource with its path, diagnostic code, and reason.
- `sync --check` then exits with status `1` if the output on disk does not match what sync would write, which would mean the result is not stable.
- `set -o pipefail` keeps a failing `sync` from being hidden by `tee`, which exits with status `0` on its own. Run these steps with bash. dash, which is `/bin/sh` on Debian and Ubuntu, rejects `set -o pipefail`.

## 4. Confirm what was discovered

Check that every resource you expect is found, in addition to checking that nothing failed:

```bash
agent-marketplace list --kind skill
agent-marketplace list --kind command
agent-marketplace list --kind subagent
agent-marketplace list --kind hook
agent-marketplace list --kind mcp
agent-marketplace list --kind collection
```

Compare the output against a checked-in expectation, for example by counting rows with `grep -c`. A resource placed in a directory that the layout does not read then fails CI instead of silently disappearing.

## 5. Stop on the first failure

Start the job script with `set -euo pipefail` so that any failing command, including one in a pipeline such as `sync | tee`, stops the job. The job must run in bash (in GitHub Actions, `shell: bash`); a plain `sh` job on Debian or Ubuntu stops at that line.

## Troubleshooting

- Diagnostics in CI differ from local runs: compare the CLI version (`agent-marketplace --version`) and the `harnesses` list.
- Sync writes for `factory` and `windsurf` only: the throwaway config has no `harnesses`, so sync falls back to the user configuration. On a fresh runner, the first command creates `~/.agents/config.yaml` with those two harnesses.
- A resource is missing from `list`: it is in a directory the source layout does not read. See the [registry layout reference](../../reference/registry-layout.md).
- A resource is malformed with an `ingest.*` code: look the code up in the [diagnostics reference](../../reference/diagnostics.md) or in [`pkg/harness-compilation/diag/codes.md`](https://github.com/Adyen/agent-marketplace/blob/main/pkg/harness-compilation/diag/codes.md).
