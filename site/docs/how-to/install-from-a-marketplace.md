# How to install a resource from a hosted Marketplace

Register a hosted Marketplace, find a resource, inspect its verified installation preview, and install its exact revision.

## Prerequisites

Before starting, ensure:

- `agent-marketplace` is on your `PATH`;
- your effective configuration selects at least one target harness; and
- you have the Marketplace endpoint URL.

Replace `https://marketplace.example.com`, `team`, `shared-agents`, and `review` in the examples with values from your Marketplace.

## 1. Declare the Marketplace source

Add a named source to the v4 configuration layer that should own it:

```yaml
apiVersion: agent-marketplace/v4
sources:
  team:
    marketplace: https://marketplace.example.com
```

The CLI verifies the endpoint's well-known document, canonical URL, and instance identity when it searches or resolves a resource. Source names must be unique within the configuration layer.

## 2. Search for a resource

Search the configured Marketplaces:

```bash
agent-marketplace search review --kind skill
```

A matching result has this form:

```text
MARKETPLACE  SOURCE          KIND   NAME    DESCRIPTION
team         shared-agents   skill  review  Review code changes
```

`SOURCE` is the registry slug used in the install locator. To search one endpoint, add `--marketplace team`. To preserve partial endpoint failures in machine-readable output, add `--output json`.

## 3. Record the exact resource request

Use the Marketplace result's source, kind, name, and exact revision to add a request to the v4 resource group:

```yaml
resources:
  team:
    include:
      - shared-agents/skill/review:
          revision: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

You can also install the resource in repository scope:

```bash
agent-marketplace install team:shared-agents/skill/review
```

Before changing configuration, the CLI fetches and verifies the immutable revision. Review its license, findings, capabilities, provenance, and acquisition mode. A resource without findings or active capabilities produces compact output:

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
  team:shared-agents/skill/review \
  --as team-review
```

The Marketplace coordinate remains `review`; generated harness output uses `team-review`.

## 5. Verify the exact pin

Inspect the installed Marketplace state:

```bash
agent-marketplace status
```

Under **MARKETPLACE PINS**, confirm that the row reports:

- alias `team`;
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

- **`no Marketplace source configured`**: Add a `marketplace` source to the v4 configuration, or search an unconfigured endpoint with `--marketplace <endpoint-url>`.
- **`marketplace "..." not found`**: Check the configured alias, or pass the endpoint URL to `--marketplace`.
- **Instance ID mismatch**: The endpoint identity changed or the URL points to a different deployment. Do not edit `instance_id` to bypass the check; confirm the endpoint with its operator.
- **Marketplace URL blocked**: The effective `allowed_marketplaces` policy does not permit the endpoint. Use an approved endpoint or ask the policy owner to update the controlling layer.
- **Revoked revision**: Install a current revision. As an emergency, execution-only escape you can run one sync with the exact revoked pin allowed: `agent-marketplace sync --allow-revoked <instance_id>/<registry>/<kind>/<name>@sha256:<64 hex>` (the first segment is the Marketplace `instance_id`, and `<kind>` for MCP servers is `mcpserver`). This never makes `sync --check` healthy, and the flag cannot be combined with `--check`.
- **Offline sync warning**: `sync --offline` uses trusted cached Marketplace data and cannot observe revocations published after the cache was populated.
