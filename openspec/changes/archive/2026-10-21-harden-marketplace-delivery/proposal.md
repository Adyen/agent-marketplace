## Why

The delivery-security review identified confusing secret-scan failure handling,
implicit mutable Helm image versions, a general-purpose web runtime, and
insufficient production guidance for overridable image inputs.

The secret scan already has a follow-up failure step; simplify it rather than
claiming that the existing workflow silently accepts scanner failures.

## What Changes

- Let the TruffleHog action fail its job directly.
- Require an explicit tag or digest for each enabled Helm workload.
- Separate the Debian Node builder from a distroless Node web runtime, keeping
  mirror overrides and shell-free startup and healthchecks.
- Require verified production digests in operator guidance for both deployment
  images and all build-image overrides, including internal mirrors.
- Add regression checks and run real Helm rendering and web-runtime smoke
  checks in the corresponding CI gates.

## Capabilities

### New Capabilities

- `marketplace-delivery-security`: Fail-closed scanning, explicit Helm image
  versions, and an independently selectable shell-free web runtime.

### Modified Capabilities

None.

## Impact

Deployment defaults, Dockerfiles, Compose, Make targets, CI, and operator docs
change. Default Helm rendering now needs an explicit image version. No consumer
configuration, acquisition lock, HTTP API, CLI, or TUI behavior changes.
