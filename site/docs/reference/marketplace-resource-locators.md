# Marketplace resource locators reference

A [locator](../glossary.md#locator) is the text you pass to `agent-marketplace install` to name one resource on a hosted Marketplace. This page gives the exact grammar of both locator forms and the rules for each part.

## Compact form

```text
<alias>:<registry>/<kind>/<name>
```

`<alias>` is the short name you gave the Marketplace endpoint when you registered it.

Example:

```text
team:shared-agents/skill/review
```

## URL form

```text
https://marketplace.example.com/r/<registry>/<kind>/<name>
```

This is the address of the resource page in the Marketplace browser. You can copy it from the page and pass it to `install`.

Example:

```text
https://marketplace.example.com/r/shared-agents/skill/review
```

The URL may use `http` or `https`, and the endpoint may have a path prefix before `/r/` (for example `https://example.com/marketplace/r/shared-agents/skill/review`). The CLI takes everything before `/r/` as the endpoint URL. The URL must not contain user information, a query, a fragment, encoded slashes or backslashes (`%2F`, `%5C`), or `.` and `..` segments.

## Exact revision

Without a revision, the CLI resolves the latest usable revision and pins its digest. To install one specific revision, add its content digest.

For a compact locator, append `@sha256:<64 lowercase hex>`:

```text
team:shared-agents/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

For a URL, append `/revisions/sha256:<64 lowercase hex>`:

```text
https://marketplace.example.com/r/shared-agents/skill/review/revisions/sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

The two suffixes are not interchangeable. `/revisions` belongs only to the URL form, and `@` belongs only to the compact form. The revision must be `sha256:` followed by exactly 64 lowercase hexadecimal characters; any other value is rejected.

## Components

| Component | Description |
|---|---|
| `alias` | Alias of a registered Marketplace endpoint. Cannot contain `@` or `:`, and cannot be `all` (reserved for `search --marketplace all`). |
| `registry` | Registry slug or ID, as used in Marketplace routes. |
| `kind` | `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. MCP servers use `mcpserver` here, not `mcp`; see [Kind spelling](#kind-spelling). |
| `name` | Resource name within the registry. |
| `revision` | Content digest of the exact revision. |

Every component must be non-empty and must not have leading or trailing whitespace, be `.` or `..`, or contain `/`, `\`, `%`, or control characters. `registry` also cannot contain `@` or `:`, and `name` can contain at most one `@` (the revision separator in the compact form).

## Kind spelling

Marketplace locators and HTTP routes use the protocol's kind names. One of them differs from the CLI `--kind` flag:

| CLI `--kind` | Locator / API `{kind}` |
|---|---|
| `skill` | `skill` |
| `command` | `command` |
| `subagent` | `subagent` |
| `hook` | `hook` |
| `mcp` | `mcpserver` |
| `collection` | `collection` |

`agent-marketplace install team:shared-agents/mcp/docs-server` fails with `invalid Marketplace resource kind "mcp"`. Use `team:shared-agents/mcpserver/docs-server` instead.

## Registration

To use a compact locator, first register the endpoint by installing its base URL:

```bash
agent-marketplace install https://marketplace.example.com
```

The CLI reads `/.well-known/agent-marketplace.json` from that URL and records the endpoint's `instance_id`. Later commands compare the `instance_id` again and refuse the endpoint if it changed. A URL-form locator names its endpoint directly, so it can be installed without registering first.

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
