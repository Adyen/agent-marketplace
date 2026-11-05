## Why

The `agent-marketplace convert` subcommand performed a one-shot harness translation with its own publication path. Harness translation already lives in the embedded `pkg/harness-compilation` library, which the local `hcc` tool also exposes, so the subcommand duplicates a capability without serving the Marketplace workflow.

## What Changes

- **BREAKING** `agent-marketplace convert` is no longer a command. Its flags (`--from`, `--to`, `--out`, `--autodetect`, `--sync`, `--dry-run`) are removed with it.
- The conversion-only publication code, source boundary handling, and their tests are deleted.
- Convert-based E2E checks exercise `pkg/harness-compilation` directly through `Convert` and `Materialize`; their assertions are kept.
- `doctor` and `status` keep the `convert` capability label; it describes harness conversion support, not the command.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registry-symlink-containment`: the requirement covering direct conversion primary reads is removed, since direct conversion no longer exists in Agent Marketplace.
- `consumer-config-format`: the clause keeping harness source-layout `convert` available alongside the removed migration surface is dropped.

## Impact

- `cmd/agent-marketplace`: command registration, migration prompt skip list, docs-contract witnesses, and benchmarks; `convert*.go` and `conversion_*.go` removed.
- `e2e`: convert scenarios move to `harness_conversion_test.go`; the convert flag test and command-coverage entry are removed.
- `scripts/security-race.sh`: the `TestConvertSource` selection is removed.
- Docs and skills: command references and the harness layout how-to are removed.
