# How to install a resource from a hosted Marketplace

This guide shows how to register a hosted Marketplace, find a resource, inspect its verified installation preview, and install its exact revision.

## Prerequisites

Before starting, ensure:

- `agent-marketplace` is on your `PATH`;
- your effective configuration selects at least one target harness; and
- you have the Marketplace endpoint URL.

Replace `https://marketplace.example.com`, `team`, `shared-agents`, and `review` in the examples with values from your Marketplace.

## 1. Register the Marketplace

Add the endpoint to your user configuration:

```bash
agent-marketplace install https://marketplace.example.com
```

The CLI reads the endpoint's well-known document, verifies its canonical URL and instance identity, and records it in `~/.agents/config.yaml`:

```text
Added Marketplace https://marketplace.example.com to <home>/.agents/config.yaml
```

Marketplace endpoints default to user scope. Use `--scope repo` or `--scope local` only when the endpoint should apply to that configuration layer.

Add an alias to the generated entry if you want shorter search and install commands:

```yaml
marketplaces:
  - url: https://marketplace.example.com
    instance_id: marketplace-production
    alias: team
```

The alias must be unique and cannot be `all`.

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

## 3. Install the selected resource

Build the compact locator from the Marketplace alias, source, kind, and name:

```text
<alias>:<source>/<kind>/<name>
```

Install the resource in repository scope:

```bash
agent-marketplace install team:shared-agents/skill/review
```

Before changing configuration, the CLI fetches and verifies the immutable revision and prints a preview similar to:

```text
Marketplace install preview:
marketplace: team (https://marketplace.example.com)
instance_id: marketplace-production
registry: shared-agents (...)
kind: skill
name: review
installed_name: review
revision: sha256:...
content_digest: sha256:...
archive_digest: sha256:...
acquisition: mirror
license: Apache-2.0
findings: none
capabilities: none
provenance: registry_url=... ref=... path=... generator=...
Installed review in this repo
```

The command writes an exact content pin under `marketplaces[].resources` in `.agents/config.yaml` and synchronizes the selected harness outputs.

You can also install a browser resource URL copied from the Marketplace:

```bash
agent-marketplace install \
  https://marketplace.example.com/r/shared-agents/skill/review
```

A URL ending in `/revisions/sha256:<digest>` installs that exact revision. Without a revision, the CLI resolves the latest usable revision and pins its digest.

## 4. Review findings and capabilities

If the verified artifact contains warning-level findings or active capabilities, the first install prints the preview and stops before editing configuration. Read the complete preview and inspect the resource source. If you accept the displayed revision and risks, rerun the same command with explicit consent:

```bash
agent-marketplace install \
  team:shared-agents/skill/review \
  --accept-risk
```

For non-interactive automation, `--yes` has the same consent effect. Both flags apply only to the exact revision verified by that invocation. A rejected or revoked revision remains blocked.

Findings are grouped by analyzer in the preview. A gating analyzer that fails to complete is reported as `marketplace.policy.audit-unavailable`; this finding is non-consentable, so `--accept-risk` and `--yes` cannot clear it. The resource becomes installable only after the analyzer succeeds.

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

- **`no marketplaces configured`**: Register an endpoint with `agent-marketplace install <endpoint-url>`, or search an unconfigured endpoint with `--marketplace <endpoint-url>`.
- **`marketplace "..." not found`**: Check the configured `alias`, or pass the endpoint URL to `--marketplace`.
- **Instance ID mismatch**: The endpoint identity changed or the URL points to a different deployment. Do not edit `instance_id` to bypass the check; confirm the endpoint with its operator.
- **Marketplace URL blocked**: The effective `allowed_marketplaces` policy does not permit the endpoint. Use an approved endpoint or ask the policy owner to update the controlling layer.
- **Revoked revision**: Install a current revision. An emergency execution-only override is available through `sync --allow-revoked` for one exact pin; it does not make `sync --check` healthy.
- **Offline sync warning**: `sync --offline` uses trusted cached Marketplace data and cannot observe revocations published after the cache was populated.
