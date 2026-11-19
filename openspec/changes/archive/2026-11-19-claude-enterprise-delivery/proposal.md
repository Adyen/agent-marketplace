## Why

Internal users who work in Claude Desktop cannot get Marketplace resources today.
`agent-marketplace` materializes resources into harness roots on a developer's
machine; Claude Desktop does not read a local plugin folder. Its plugin list is a
downloaded cache of the member's claude.ai account
(`~/Library/Application Support/Claude/plugin-list/<account>/<org>/plugins.json`,
with install paths under app-private session directories). The account is the
only supported input.

Issue #960 proposed closing that gap from the client: a Chromium profile owned by
agent-marketplace, a separate claude.ai sign-in per user, and uploads through
undocumented `account-upload` endpoints into each member's personal **My Uploads**
marketplace. That design duplicates a login Claude Desktop already holds, makes a
browser profile directory a credential, depends on undocumented endpoints, and
re-uploads the same bytes once per member (the endpoint does no deduplication).

Anthropic documents a supported path for exactly this case. The
[Plugins API](https://platform.claude.com/docs/en/manage-claude/plugins-api)
(Claude Enterprise, Admin API key, `anthropic-beta: ce-plugins-2026-09-01`) lets
an organization upload plugins into an organization-owned `manual` marketplace and
set who may install them. Plugins are saved to the member's account, so Claude
Desktop, Cowork, and Claude Code all pick them up with no local install and no
second sign-in.

The Marketplace service is already the component that knows which resource
revisions exist, which ones policy allows, and which ones are revoked. It is the
right place to hold the one organization credential and push on the organization's
behalf, instead of asking every member to authenticate a browser.

## What Changes

- `agent-marketplace serve` gains an opt-in **Claude Enterprise delivery target**.
  When configured, it mirrors every published resource revision that policy allows
  and that is not revoked into one organization-owned `manual` marketplace in a
  Claude Enterprise organization, and keeps it in step as registries refresh.
- Each mirrored resource becomes **one Claude plugin**, so members install at the
  same granularity the Marketplace already offers. Plugin names are derived
  deterministically from the resource coordinate and are stable for the lifetime of
  the plugin.
- Archives are built deterministically from the already-published canonical
  artifact into the Claude plugin source layout
  (`.claude-plugin/plugin.json`, `skills/`, `commands/`, `agents/`, `hooks/`,
  `.mcp.json`) and validated locally against Anthropic's documented upload
  requirements before any request is sent. A resource that cannot produce a valid
  archive is skipped with a stable diagnostic code; it never reaches the API.
- Content that has not changed is never re-uploaded. The API creates a new version
  for identical bytes, so change detection is digest-based and owned by us.
- Each plugin gets an explicit organization-wide installation preference,
  `available` by default. The marketplace default is never written, so plugins
  uploaded by administrators in claude.ai are unaffected.
- A resource that stops being published, is rejected by policy, or is revoked is
  withdrawn by setting its plugin to `not_available`. Plugins are never deleted.
- Delivery runs in the existing worker as a leased, retryable job: after each
  registry publication, when revocations change, on a repair interval, and on
  demand. It is paced under the documented rate limits and follows the documented
  retry rules, including the `registration_pending` case where an upload was stored
  despite the error.
- Operators get `agent-marketplace serve claude-delivery status` and
  `--sync-once`, a read-only admin endpoint, and metrics for reconcile outcomes,
  per-plugin state, and API responses.

## Impact

- **New capability**: `claude-enterprise-delivery`.
- **Affected code**: `pkg/marketplace/config` (new `claude_enterprise` block),
  `pkg/marketplace/store` (delivery state schema and queries, SQLite and
  PostgreSQL), `pkg/marketplace/worker` (new job kind and reconciler),
  `pkg/marketplace/api` (admin read endpoint),
  new `pkg/marketplace/claudeplugins` (Plugins API client) and
  `pkg/marketplace/claudedelivery` (archive build, validation, reconcile),
  `cmd/agent-marketplace/serve_claude_delivery.go`.
- **Operator action**: a Claude Enterprise plan, an Admin API key with
  `read:plugins` and `write:plugins` created by the primary owner, and a decision
  about which organization-owned `manual` marketplace receives the uploads.
- **No consumer change**: no client configuration, lock, or harness output changes.
  Members need nothing installed locally.

## Release note

Marketplace operators can mirror published resources into a Claude Enterprise
organization with `serve`. Delivery is off by default. Withdrawal stops new
installations without removing installed copies or stored version history.
