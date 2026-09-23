# How to install a resource from a hosted Marketplace

Find a resource on a hosted Marketplace and install an exact revision after reviewing the CLI's verified preview.

## Prerequisites

You need:

- `agent-marketplace` on your `PATH`;
- at least one target harness selected in your effective configuration;
- the Marketplace endpoint URL; and
- a destination config that uses editable v2/v3, or no destination config yet. Install commands cannot edit v4 files; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

Replace `https://marketplace.example.com`, `shared-agents`, and `review` in the examples with values from your Marketplace.

## 1. Register the Marketplace endpoint

Register the endpoint for this repository:

```bash
agent-marketplace install https://marketplace.example.com --scope repo
```

Before saving the endpoint, the CLI checks its well-known document, canonical URL, and instance identity. Registration does not install resources. Omit `--scope repo` to register in user scope. If the same identity is already registered in that scope or a broader one, the command leaves it unchanged.

If you already have a resource URL from the [browser](./browse-marketplace-resources.md), you can skip registration and install that URL directly in step 3.

## 2. Search for a resource

Search the configured Marketplaces:

```bash
agent-marketplace search review --kind skill --marketplace https://marketplace.example.com
```

Results have columns for `MARKETPLACE`, `SOURCE`, `KIND`, `NAME`, and `DESCRIPTION`. Use the registry slug in `SOURCE` when building the install locator. Omit `--marketplace` to search all configured endpoints, or replace the URL with a configured alias. Add `--output json` for machine-readable results that include errors from endpoints that failed.

## 3. Install the resource

Use the result's source, kind, and name to install in repository scope:

```bash
agent-marketplace install shared-agents/skill/review \
  --marketplace https://marketplace.example.com --scope repo
```

The CLI fetches and verifies the immutable revision before changing config. Use `--verbose` to see its license, findings, capabilities, provenance, and acquisition mode. If it has no findings or active capabilities, the output looks like this:

```text
Installing review · skill

Source: shared-agents

✓ Installed review
```

Collections list their included resources, grouped by kind. The default output shows up to ten members; use `--verbose` to show the complete inventory and technical preview, including revisions, digests, acquisition details, and provenance.

The command writes an exact content pin under the Marketplace resources and synchronizes the selected harness outputs. You can also install a browser resource URL copied from the Marketplace:

```bash
agent-marketplace install \
  https://marketplace.example.com/r/shared-agents/skill/review
```

A URL ending in `/revisions/sha256:<digest>` installs that exact revision. Without a revision, the CLI resolves the latest usable revision and pins its digest.

## 4. Review findings and capabilities

If the verified artifact contains warning-level findings or active capabilities, an interactive terminal shows the compact install details and asks for confirmation:

```text
Installing review · skill

Source: shared-agents

Warning: 1 security finding
  HIGH  [review] Executes shell commands
Active capabilities: shell

Install despite these risks? [y/N]:
```

Answer `y` to install the prepared revision without fetching it again. After acceptance, the CLI prints only the concise success result.

For non-interactive automation, pass `--accept-risk` or `--yes`. Both flags apply only to the exact revision verified by that invocation. Without either flag the command returns a nonzero exit code and installs nothing. A rejected or revoked revision remains blocked.

If the resource name conflicts with an existing generated path, choose a safe local name:

```bash
agent-marketplace install \
  shared-agents/skill/review \
  --marketplace https://marketplace.example.com \
  --as team-review
```

The Marketplace coordinate remains `review`; generated harness output uses `team-review`.

## 5. Verify the exact pin

Inspect the installed Marketplace state:

```bash
agent-marketplace status
```

Under **MARKETPLACE PINS**, confirm that the row reports:

- the intended Marketplace endpoint (or its configured alias);
- source `shared-agents`;
- kind `skill` and name `review`;
- state `current`;
- a `sha256:` content digest; and
- the intended configuration layer.

Then verify that configuration and generated output are in sync:

```bash
agent-marketplace sync --check
```

If everything is in sync, the check exits with status 0 and prints no drift verdict. If it finds drift, it exits with status 1 and lists the changes needed.

## Troubleshooting

- `no Marketplace source configured`: Register the endpoint as in step 1, or search an unconfigured endpoint with `--marketplace <endpoint-url>`.
- `v4.writer-opt-in`: A legacy-only writer was used on a v4 root. Current CLI endpoint registration and explicit Marketplace install/remove commands use v4-aware config-and-lock transactions; rebuild or upgrade the CLI if one of those commands reports this code, and report the failing command. Do not change `apiVersion` or write acquisition evidence by hand.
- `dependency transaction: generation conflict` while copying a hosted pin in the TUI: The selected pin, target binding, or source acquisition lock changed after the row was loaded. Reload the TUI. If only legacy evidence is missing, the TUI opens a verified install preview instead; rejecting it writes nothing, and confirmation saves the target v4 config/lock pair with output pending sync. An unchanged stale legacy source pin can still block that sync.
- `marketplace "..." not found`: Check the configured alias, or pass the endpoint URL to `--marketplace`.
- Instance ID mismatch: The endpoint identity changed or the URL points to a different deployment. Do not edit `instance_id` to bypass the check; confirm the endpoint with its operator.
- Marketplace URL blocked: The effective `allowed_marketplaces` policy does not permit the endpoint. Use an approved endpoint or ask the policy owner to update the controlling layer.
- Revoked revision: Install a current revision. In an emergency, you can allow the exact revoked pin for one sync: `agent-marketplace sync --allow-revoked <instance_id>/<registry>/<kind>/<name>@sha256:<64 hex>` (the first segment is the Marketplace `instance_id`, and `<kind>` for MCP servers is `mcpserver`). This never makes `sync --check` healthy, and the flag cannot be combined with `--check`.
- Offline sync warning: `sync --offline` uses trusted cached Marketplace data and cannot observe revocations published after the cache was populated.
