# How to validate a registry in CI

Make a registry's CI job fail when a resource it ships is malformed, missing from discovery, or cannot be compiled for the harnesses your consumers use.

## Prerequisites

- A CI runner with Git, plus either network access to GitHub releases or Go to build from source.
- A checkout of the registry repository.
- The registry's source layout (`claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, or `codex`).

## 1. Install a pinned CLI

Pin the version so CI results are reproducible and match what consumers run. Choose the release your consumers use and pin that exact tag; do not track the newest release automatically, because a new release can change diagnostics and make an unchanged registry fail. The examples pin v4.1.3. Run the job in bash and prepare a job-owned bin directory first, so either installation path below works:

```bash
set -euo pipefail
BIN_DIR="$(mktemp -d)"
export PATH="$BIN_DIR:$PATH"
```

The three prep commands succeed silently. Then download the release asset for the pinned version:

```bash
# Download a pinned release binary (Linux runner example)
VERSION="v4.1.3"
ARCH="linux-amd64" # or linux-arm64, darwin-arm64, darwin-amd64
curl -fsSL "https://github.com/Adyen/agent-marketplace/releases/download/${VERSION}/agent-marketplace-${VERSION}-${ARCH}" -o "$BIN_DIR/agent-marketplace"
chmod +x "$BIN_DIR/agent-marketplace"
agent-marketplace --version
```

Or build from source at the pinned tag:

```bash
SOURCE_DIR="$(mktemp -d)"
git clone --depth 1 --branch v4.1.3 https://github.com/Adyen/agent-marketplace.git "$SOURCE_DIR"
(cd "$SOURCE_DIR" && CGO_ENABLED=0 go build -ldflags "-X main.version=v4.1.3" -o "$BIN_DIR/agent-marketplace" ./cmd/agent-marketplace)
agent-marketplace --version
```

`curl`, `chmod`, and `go build` succeed silently; `git clone` prints progress lines. Either way, the last command prints `v4.1.3`. The `-ldflags` value makes `agent-marketplace --version` print the tag. Remove the `mktemp` directories at the end of the job unless your runner cleans its workspace; both hold only files this job created. There is no Windows build.

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
    directory: "${REGISTRY_PATH}"
resources:
  checkout:
    all: true
    required: true
EOF
```

The commands succeed silently and write the throwaway consumer config at `"$CONSUMER/.agents/config.yaml"`.

- List every harness your consumers target. A resource that compiles for Claude Code can still produce an error for Codex or a warning for Windsurf.
- A `directory` source reads the checkout directly and creates no lockfile, which is what you want in CI.
- `all: true` selects every resource of every kind. `required: true` stops an exclusion in another configuration layer on the runner from hiding a resource.

## 3. Sync strictly and check the summary

```bash
cd "$CONSUMER"
agent-marketplace sync --strict -v | tee sync.log
grep -q 'Malformed 0  Errors' sync.log || { echo "registry has malformed resources" >&2; exit 1; }
agent-marketplace sync --check
```

A clean sync ends with a summary such as `Installed 12  Removed 0  Malformed 0  Errors 0` (counts vary with the registry). `sync --check` exits with status `0` when there is no drift; warnings may still print.

- `sync` exits with status `1` when any resource fails to compile for a selected harness (`Errors` greater than `0`). Warnings, such as a subagent that Windsurf cannot represent, do not fail the run.
- `--strict` makes sync exit with status `1` when a resource named in a configuration or a collection is missing or fails to load. `all: true` also selects every collection, so this checks that every collection member exists.
- With `all: true`, a resource that fails to load is left out of the catalog and counted under `Malformed` without failing the run, even with `--strict`. The `grep` line catches it. `-v` lists each malformed resource with its path and error.
- `sync --check` then exits with status `1` if the output on disk does not match what sync would write, which would mean the result is not stable.
- `pipefail`, set at the start of the job, keeps a failing `sync` from being hidden by `tee`, which exits with status `0` on its own. Run the job with bash (`shell: bash` in GitHub Actions); dash, which is `/bin/sh` on Debian and Ubuntu, rejects `set -o pipefail`.

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

Each command prints a `REGISTRY  KIND  NAME  TAGS (metadata)  STATUS` table. `REGISTRY` shows the registry directory's base name, not the source key; typical output:

```text
REGISTRY               KIND   NAME         TAGS (metadata)  STATUS
<registry-directory>   skill  code-review  review,quality   available
```

Check a row for every resource you expect: compare the printed `NAME` and `KIND` values against a checked-in list of expected resources, and make that comparison exit nonzero when an expected resource is missing. `list` exits `0` whether or not the table contains the row, so without the check a resource placed in a directory the layout does not read would silently ship missing.

## Troubleshooting

- Diagnostics in CI differ from local runs: compare the CLI version (`agent-marketplace --version`) and the `harnesses` list.
- Sync writes for `factory` and `windsurf` only: the throwaway config has no `harnesses`, so sync falls back to the user configuration. On a fresh runner, the first command creates `~/.agents/config.yaml` with those two harnesses.
- A resource is missing from `list`: it is in a directory the source layout does not read. See the [registry layout reference](../../reference/registry-layout.md).
- A resource is malformed with an `ingest.*` code: look the code up in the [diagnostics reference](../../reference/diagnostics.md) or in [`pkg/harness-compilation/diag/codes.md`](https://github.com/Adyen/agent-marketplace/blob/main/pkg/harness-compilation/diag/codes.md).
