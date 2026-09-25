# How to validate a registry in CI

Check that a registry builds and produces no unexpected diagnostics before tagging a release.

## Prerequisites

- A CI runner with `agent-marketplace` on its `PATH`.
- A checkout of the registry repository.

## 1. Install the CLI in CI

Download a released binary or use `go install`:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
```

## 2. Capture the registry checkout

Record the registry path before changing directory:

```bash
REGISTRY_PATH=$PWD
```

## 3. Create a minimal consumer configuration

In the CI job, create a temporary configuration that points at the current checkout:

```bash
mkdir -p /tmp/consumer/.agents
cat > /tmp/consumer/.agents/config.yaml << EOF
apiVersion: agent-marketplace/v2
harnesses:
  - claude
  - factory
registries:
  - path: ${REGISTRY_PATH}
    required: true
EOF
```

## 4. Sync then check

A fresh consumer must first materialize the registry, then verify it is in sync:

```bash
cd /tmp/consumer
agent-marketplace sync --strict
agent-marketplace sync --check
```

Expected exit status: `0` after both commands if the registry is valid.

## 5. Convert to every target

For extra coverage, convert a representative sample to each target harness:

```bash
for target in claude factory cursor windsurf gemini devin codex; do
  agent-marketplace convert \
    --from claude \
    --to "$target" \
    --out "/tmp/out-$target" \
    "${REGISTRY_PATH}/skills"
done
```

## 6. Fail on errors

Configure the CI job to fail on any non-zero exit status. This catches malformed resources before a release.

## Troubleshooting

- **Diagnostics in CI differ from local**: Ensure the CI environment uses the same `harnesses` list and the same `agent-marketplace` version.
- **`sync --check` fails with missing target**: Set `harnesses` explicitly in the test configuration.
