# How to validate a registry in CI

Fail a registry's CI when any resource it ships is malformed or cannot be compiled for the harnesses your consumers use.

## Prerequisites

- A CI runner with Git and Go, or a way to download a released `agent-marketplace` binary.
- A checkout of the registry repository.
- You know the registry's source layout (`claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, or `codex`).

## 1. Install a pinned CLI

Pin the version so CI results are reproducible and match what consumers run. Either download the release asset for that version, or:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@v3.1.1
```

Avoid `@latest` in CI: a new release can change diagnostics and make an unchanged registry fail.

## 2. Point a throwaway consumer at the checkout

```bash
REGISTRY_PATH=$PWD
CONSUMER=$(mktemp -d)
mkdir -p "$CONSUMER/.agents"
cat > "$CONSUMER/.agents/config.yaml" << EOF
apiVersion: agent-marketplace/v2
harnesses:
  - claude
  - factory
  - cursor
  - windsurf
  - gemini
  - devin
  - codex
registries:
  - path: ${REGISTRY_PATH}
    source_harness: claude
    required: true
EOF
```

- List every harness your consumers target; a resource that compiles for Claude may emit a diagnostic for Codex or Windsurf.
- Set `source_harness` explicitly so CI does not depend on layout autodetection.
- `required: true` selects every resource of every kind, so nothing is silently skipped.

A local-directory registry is read live and produces no lock, which is what you want in CI.

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
