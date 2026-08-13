## 1. Correct the E2E contract

- [x] 1.1 Replace the post-removal `_managed_by` assertion in `mcp/remove_roundtrip` with assertions that `/root/.factory/mcp.json` exists, contains an empty `mcpServers` object, and contains neither the removed server nor an ownership marker.

## 2. Validate removal behavior

- [x] 2.1 Run `E2E_SKIP_DOCKER=1 go test -run 'TestE2E/mcp/remove_roundtrip' -v -count=1` from `e2e/`.
- [x] 2.2 Run the related default-removal scenario: `E2E_SKIP_DOCKER=1 go test -run 'TestE2E/mcp/remove_default_true_adds_exclude' -v -count=1` from `e2e/`.
- [x] 2.3 Run `go test ./internal/materialize ./cmd/agent-marketplace`.
