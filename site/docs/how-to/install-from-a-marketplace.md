# How to install a resource from a hosted Marketplace

Find a resource on a hosted [Marketplace](../glossary.md#marketplace) and install one exact revision of it. The CLI verifies the revision and shows a preview before it changes any configuration.

## Prerequisites

You need:

- `agent-marketplace` on your `PATH`.
- At least one target harness selected in your effective configuration.
- The Marketplace endpoint URL.

Every existing consumer root, including organization and personal-local, must declare exact `apiVersion: agent-marketplace/v4`. Included fragments may omit the field or declare exact v4. If the destination is absent, install creates v4 only after validating every required root/include tree. Unsupported input refuses before Marketplace acquisition or new config/lock/harness changes. [Convert older configurations with an older released migrator before upgrading](./migrate-configuration.md); the new CLI has no migration command or prompt.

Replace `https://marketplace.example.com`, `shared-agents`, and `review` in the examples with values from your Marketplace.

## 1. Register the Marketplace endpoint

Register the endpoint for this repository:

```bash
agent-marketplace install https://marketplace.example.com --scope repo
```

Before it saves the endpoint, the CLI checks the endpoint's well-known document, its canonical URL, and its instance identity. On success it prints `Added Marketplace <url> to <config path>`. Registering an endpoint does not install any resources.

Leave out `--scope repo` to register the endpoint in user scope instead. If the same identity is already registered in that scope or a broader one, the command leaves the config unchanged and says the Marketplace is already configured.

If you already have a resource URL from the [browser](./browse-marketplace-resources.md), you can skip registration and install that URL directly in step 3.

## 2. Search for a resource

Search the configured Marketplaces:

```bash
agent-marketplace search review --kind skill --marketplace https://marketplace.example.com
```

Typical output:

```text
MARKETPLACE                     SOURCE         KIND   NAME    DESCRIPTION
marketplace.example.com         shared-agents  skill  review  Reviews pull requests
```

The `SOURCE` column holds the registry slug, which you need for the install locator.

To search every configured endpoint, leave out `--marketplace`. You can also pass a configured alias instead of the URL. Add `--output json` for machine-readable results, which also list errors from endpoints that failed.

## 3. Install the resource

Build the resource address from the endpoint URL and the source, kind, and name in the search result. The pattern is `<endpoint>/r/<source>/<kind>/<name>`, which is also the address the Marketplace browser shows for the resource. Install it in repository scope:

```bash
agent-marketplace install https://marketplace.example.com/r/shared-agents/skill/review \
  --scope repo
```

The CLI fetches and verifies the immutable revision before it changes the config. If the verified revision has no security findings, the install goes ahead without asking, even when it lists capabilities. The output looks like this:

```text
Installing review · skill

Source: shared-agents

✓ Installed review
```

Add `--verbose` to see the full technical preview: license, findings, capabilities, provenance, acquisition mode, revisions, and digests.

For a collection, the output lists the included resources, grouped by kind when the collection mixes kinds. The default output shows up to ten members and then `… and N more (use --verbose)`.

The command writes an exact content pin into the config and then syncs the selected harness outputs.

A URL that ends in `/revisions/sha256:<digest>` installs that exact revision. Without a revision, the CLI picks the latest usable revision and pins its digest.

Once the endpoint is registered, you can use its name from the `MARKETPLACE` column instead of the URL. For an endpoint registered under the name `marketplace`:

```bash
agent-marketplace install marketplace:shared-agents/skill/review --scope repo
```

It prints the same `Installing review · skill` / `✓ Installed review` output as the URL form.

## 4. Review findings and capabilities

If the verified revision has any security findings, the CLI shows them and asks you to confirm. Capabilities are listed in the same summary, but they do not require confirmation on their own:

```text
Installing review · skill

Source: shared-agents

Warning: 1 security finding
  HIGH  [review] Executes shell commands
Active capabilities: shell

Install despite these risks? [y/N]:
```

Answer `y` to install the revision the CLI already verified. It does not fetch it again. Any other answer prints `Nothing has been installed.` and leaves the config unchanged.

When the command is not running in an interactive terminal, it prints the same summary and `Nothing has been installed.`, then exits with the error `marketplace install requires risk acceptance: 1 security finding(s); re-run with --accept-risk (or --yes)`. For automation, pass `--accept-risk` or `--yes` to accept the findings. Both flags apply only to the exact revision verified during that run.

Some revisions cannot be accepted at all. If Marketplace policy rejected a finding, the summary starts with `Blocked:` instead of `Warning:`, the error says `--accept-risk cannot bypass policy rejection`, and no flag or answer installs it. A revoked revision is refused as well.

If the resource name clashes with an existing generated path, choose a different local name:

```bash
agent-marketplace install \
  https://marketplace.example.com/r/shared-agents/skill/review \
  --as team-review
```

The Marketplace coordinate stays `review`. The generated harness output uses `team-review`; the success line reads `✓ Installed review`.

## 5. Verify the exact pin

Check the installed Marketplace state:

```bash
agent-marketplace status
```

In the `MARKETPLACE PINS` table, check that the row for the resource shows:

- The intended endpoint in `URL` (and its alias in `ALIAS`, if you set one).
- `shared-agents` in `REGISTRY_SLUG`.
- `skill` in `KIND` and `review` in `NAME`.
- `current` in `STATE`.
- A `sha256:` digest in `CONTENT_DIGEST`.
- The intended configuration layer in `LAYER`.

Then confirm the config and output match:

```bash
agent-marketplace sync --check
```

When everything is in sync, the command prints nothing apart from any warnings and exits with status 0. When it finds drift, it prints a line such as `sync: 2 out of sync (2 missing)`, lists the affected output, and exits with status 1.

## Troubleshooting

- `no marketplaces configured; add one to your config or use --marketplace <url>`: Register the endpoint as in step 1, or pass the endpoint URL to `--marketplace` to search it without registering it.
- `marketplace "..." not found`: The value passed to `search --marketplace` is not a configured alias or URL. Check the alias, or pass the endpoint URL instead.
- `marketplace alias "..." is not configured`: The name before the colon in the locator, or the value passed to `install --marketplace`, is not in your config. Register the endpoint, or install with the resource's full URL.
- `instance ID mismatch`: The endpoint's identity changed, or the URL points to a different deployment. Do not edit `instance_id` to get past the check. Confirm the endpoint with its operator.
- `marketplace URL(s) blocked by allowed_marketplaces policy`: The effective `allowed_marketplaces` policy does not allow the endpoint. Use an approved endpoint, or ask the owner of the controlling configuration layer to update the policy.
- Revoked revision: Install a current revision. In an emergency, you can allow one exact revoked pin for a single sync:

  ```bash
  agent-marketplace sync --allow-revoked <instance_id>/<registry_id>/<kind>/<name>@sha256:<64 lowercase hex>
  ```

  Take `<instance_id>`, `<registry_id>`, and the digest from the `INSTANCE_ID`, `REGISTRY_ID`, and `CONTENT_DIGEST` columns of `agent-marketplace status`. For MCP servers, `<kind>` is `mcpserver`. When the coordinate matches, the sync proceeds and prints the usual `Installed ...` summary. The override lasts only for that run. It never makes `sync --check` pass, and you cannot combine it with `--check`.
- Offline sync warning: `sync --offline` uses trusted cached Marketplace data, so it cannot see revocations published after the cache was filled.
