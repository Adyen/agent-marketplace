## Why

`sync --verbose` printed each malformed resource's parse error verbatim. Parser errors embed authored bytes such as a decoded JSON object key, so a malformed resource could clear the operator's terminal or forge extra output lines. `status` already prints the same errors escaped (#715, transferred from R4 as F101).

## What Changes

- The `sync --verbose` malformed-resource list escapes each resource name, source path and parse error with the terminal-safe policy `status` uses.

## Capabilities

### Modified Capabilities

- `command-error-output`: malformed-resource details cannot control the terminal.

## Impact

`cmd/agent-marketplace/sync.go` only. Printable output is unchanged; control characters and line breaks inside an entry become visible escapes, so each malformed resource stays on one line, as in `status`.
