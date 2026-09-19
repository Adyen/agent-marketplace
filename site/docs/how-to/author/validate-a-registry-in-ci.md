# How to validate a registry in CI

Fail a registry's CI when any resource it ships is malformed or cannot be compiled for the harnesses your consumers use.

## Prerequisites

- A CI runner with Git and Go, or a way to download a released `agent-marketplace` binary.
- A checkout of the registry repository.
- You know the registry's source layout (`claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, or `codex`).

## 1. Install a pinned CLI

Pin the version so CI results are reproducible and match what consumers run. Download the release asset for that version, or build from source at a pinned tag:

```bash
# Download a pinned release binary (Linux runner example)
VERSION="v3.1.1"
ARCH="linux-amd64" # or linux-arm64, darwin-arm64, etc.
curl -fsSL "https://github.com/Adyen/agent-marketplace/releases/download/${VERSION}/agent-marketplace-${VERSION}-${ARCH}" -o /usr/local/bin/agent-marketplace
chmod +x /usr/local/bin/agent-marketplace
```

Or build from source at the pinned release tag:

```bash
git clone --depth 1 --branch v3.1.1 https://github.com/Adyen/agent-marketplace.git /tmp/agent-marketplace
(cd /tmp/agent-marketplace && go build -o /usr/local/bin/agent-marketplace ./cmd/agent-marketplace)
rm -rf /tmp/agent-marketplace
```

Avoid unpinned builds or `@latest` in CI: a new release can change diagnostics and make an unchanged registry fail.

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

- List every harness your consumers target; a resource that compiles for Claude may emit a diagnostic for Codex or Windsurf.
- A local-directory source is read live and produces no lock, which is what you want in CI.
- `required: true` selects every resource of every kind, so nothing is silently skipped.

## 3. Sync strictly, then check

```bash
cd "$CONSUMER"
agent-marketplace sync --strict -v
agent-marketplace sync --check
```

`--strict` turns "selected but malformed" and "selected but missing" warnings into a non-zero exit; `-v` prints the diagnostic code and file for each one. `sync --check` then confirms the materialized output is stable.

## 4. Confirm what was discovered

Make sure every resource you expect is actually found, not merely that nothing failed:

```bash
agent-marketplace list --kind skill
agent-marketplace list --kind command
agent-marketplace list --kind subagent
agent-marketplace list --kind hook
agent-marketplace list --kind mcp
```

Compare against a checked-in expectation (for example `grep -c` on the output) so a resource that lands in an undiscovered directory fails CI instead of vanishing.

## 5. Fail on any non-zero status

Run the job with `set -euo pipefail` so any failing command stops the pipeline.

## Troubleshooting

- **Diagnostics in CI differ from local**: Confirm the same CLI version and the same `harnesses` list. `agent-marketplace --version` prints the version.
- **`sync --check` fails with missing target**: `harnesses` is missing from the consumer config.
- **A resource is silently absent**: It is in a directory the source harness does not discover. Check the [registry layout reference](../../reference/registry-layout.md).
- **`Malformed` with an `ingest.*` code**: Look the code up in `pkg/harness-compilation/diag/codes.md` in the CLI repository.
