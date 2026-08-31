#!/usr/bin/env bash
set -euo pipefail

# Build a release binary with configurable community telemetry defaults.
# Set AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT and/or
# AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL to inject production values
# at link time. Do not hard-code real URLs or secrets in this script.

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ENDPOINT="${AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT:-}"
RETENTION="${AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL:-}"

CGO_ENABLED=0 go build \
  -ldflags "\
    -X main.version=${VERSION} \
    -X github.com/Adyen/agent-marketplace/internal/telemetry.BuildVersion=${VERSION} \
    -X github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectEndpoint=${ENDPOINT} \
    -X github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectRetentionURL=${RETENTION} \
  " \
  -o agent-marketplace \
  ./cmd/agent-marketplace
