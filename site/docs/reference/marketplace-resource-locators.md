# Marketplace resource locators reference

## Compact form

```text
<alias>:<registry>/<kind>/<name>
```

Example:

```text
team:shared-agents/skill/review
```

## URL form

```text
https://marketplace.example.com/r/<registry>/<kind>/<name>
```

Example:

```text
https://marketplace.example.com/r/shared-agents/skill/review
```

## Exact revision

For a compact locator, append `@sha256:<64-lowercase-hex>`:

```text
team:shared-agents/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

For a URL, append `/revisions/sha256:<64-lowercase-hex>`:

```text
https://marketplace.example.com/r/shared-agents/skill/review/revisions/sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Do not append `/revisions` to the compact form. `/revisions` is the URL path suffix, not a compact locator segment.

## Components

| Component | Description |
|---|---|
| `alias` | User-defined endpoint alias. Cannot be `all`. |
| `registry` | Route-facing registry slug or ID. |
| `kind` | `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. Note `mcpserver`, not `mcp`: the CLI `--kind` flag uses `mcp`, but Marketplace locators and HTTP routes use the protocol spelling. |
| `name` | Marketplace resource name. |
| `revision` | Content digest of the exact revision. |

## Kind spelling

Marketplace locators use the wire protocol's kind names, which differ from the CLI `--kind` flag for one kind:

| CLI `--kind` | Locator / API `{kind}` |
|---|---|
| `skill` | `skill` |
| `command` | `command` |
| `subagent` | `subagent` |
| `hook` | `hook` |
| `mcp` | `mcpserver` |
| `collection` | `collection` |

`agent-marketplace install team:shared-agents/mcp/docs-server` is rejected as an invalid kind; use `team:shared-agents/mcpserver/docs-server`.

## Registration

Register an endpoint by installing its base URL:

```bash
agent-marketplace install https://marketplace.example.com
```

The CLI verifies `instance_id` against `/.well-known/agent-marketplace.json`.

## CLI usage

Install the latest usable revision:

```bash
agent-marketplace install team:shared-agents/skill/review
```

Install an exact revision with the compact form:

```bash
agent-marketplace install \
  'team:shared-agents/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
```

## Related documentation

- [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md)
- [Marketplace HTTP API reference](./marketplace-http-api.md)
