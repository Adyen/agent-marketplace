# How to install a resource from a hosted Marketplace

Register a hosted Marketplace, find a resource, inspect its verified installation preview, and install its exact revision.

## Prerequisites

Before starting, ensure:

- `agent-marketplace` is on your `PATH`;
- your effective configuration selects at least one target harness;
- you have the Marketplace endpoint URL; and
- the destination config is editable v2/v3, or does not exist yet. Resource-install editors cannot write v4 roots; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

Replace `https://marketplace.example.com`, `shared-agents`, and `review` in the examples with values from your Marketplace.

## 1. Register the Marketplace endpoint

Register the endpoint for this repository:

```bash
agent-marketplace install https://marketplace.example.com --scope repo
```

The CLI verifies the endpoint's well-known document, canonical URL, and instance identity before recording it. Registration installs no resources. Without `--scope repo`, endpoint registration defaults to user scope; an identity already registered in the same or a broader scope is left unchanged.

If you already have a resource URL from the [browser](./browse-marketplace-resources.md), you can skip registration and install that URL directly in step 3.

## 2. Search for a resource

Search the configured Marketplaces:

```bash
agent-marketplace search review --kind skill --marketplace https://marketplace.example.com
```

Results include `MARKETPLACE`, `SOURCE`, `KIND`, `NAME`, and `DESCRIPTION`. `SOURCE` is the registry slug used in the install locator. Omit `--marketplace` to search all configured endpoints, or use an existing configured alias instead of the URL. To preserve partial endpoint failures in machine-readable output, add `--output json`.

## 3. Install the resource

Use the result's source, kind, and name to install in repository scope:

```bash
agent-marketplace install shared-agents/skill/review \
  --marketplace https://marketplace.example.com --scope repo
```

Before changing configuration, the CLI fetches and verifies the immutable revision. Use `--verbose` to review its license, findings, capabilities, provenance, and acquisition mode. A resource without findings or active capabilities produces compact output:

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

An in-sync check returns exit status 0 without a drift verdict. Drift returns exit status 1 and lists the planned changes.

## Troubleshooting

- **`no Marketplace source configured`**: Register the endpoint as in step 1, or search an unconfigured endpoint with `--marketplace <endpoint-url>`.
- **`v4.writer-opt-in`**: The selected destination is v4 and cannot be edited by resource-install commands. Do not change only its `apiVersion` or hand-author lock evidence. Keep CLI-managed selections in an appropriate editable layer, or follow the [v4 migration and maintenance guide](./migrate-configuration.md). General acquisition of new hosted v4 requests is not implemented.
- **`marketplace "..." not found`**: Check the configured alias, or pass the endpoint URL to `--marketplace`.
- **Instance ID mismatch**: The endpoint identity changed or the URL points to a different deployment. Do not edit `instance_id` to bypass the check; confirm the endpoint with its operator.
- **Marketplace URL blocked**: The effective `allowed_marketplaces` policy does not permit the endpoint. Use an approved endpoint or ask the policy owner to update the controlling layer.
- **Revoked revision**: Install a current revision. As an emergency, execution-only escape you can run one sync with the exact revoked pin allowed: `agent-marketplace sync --allow-revoked <instance_id>/<registry>/<kind>/<name>@sha256:<64 hex>` (the first segment is the Marketplace `instance_id`, and `<kind>` for MCP servers is `mcpserver`). This never makes `sync --check` healthy, and the flag cannot be combined with `--check`.
- **Offline sync warning**: `sync --offline` uses trusted cached Marketplace data and cannot observe revocations published after the cache was populated.
