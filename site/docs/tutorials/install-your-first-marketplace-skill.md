# Install your first skill from an online Marketplace

Find a skill in your organization's hosted Marketplace, install it for Claude Code, and verify the exact revision you received. You will work in a throwaway repository and save the new selection there, rather than installing it machine-wide.

Unlike the [Git registry tutorial](./install-your-first-skill.md), this lesson starts with a searchable online catalog. You do not need to configure the skill's backing Git repository.

## Before you begin

You need:

- `agent-marketplace` on your `PATH` ([installation instructions](../install.md));
- Git and Claude Code; and
- access to your organization's Marketplace from both your browser and the CLI, including any required VPN or operator-provided access setup.

**There is no public Marketplace for this lesson.** `https://marketplace.example.com` is a placeholder, not a working service. Ask your Marketplace operator for its endpoint and a published, approved skill suitable for a first installation, with no warning findings or active capabilities. The examples use a code-review skill named `review` in the `shared-agents` registry. Substitute your endpoint, registry slug, and skill name throughout; they are inputs to the same lesson, not alternative installation methods.

Confirm that the CLI is available:

```bash
agent-marketplace --help
```

Expect a command list containing `install`, `search`, `status`, and `sync`.

## 1. Prepare a practice repository

Create a temporary directory and initialize a Git repository:

```bash
mkdir -p /tmp
cd "$(mktemp -d /tmp/marketplace-tutorial.XXXXXX)"
git init --quiet
pwd
```

The last command prints a path like `/tmp/marketplace-tutorial.a1B2c3` (on macOS, the resolved path may begin with `/private/tmp`). Keep this terminal in that directory for the rest of the lesson.

Select Claude Code for this repository:

```bash
mkdir .agents
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v2
harnesses: [claude]
registries: []
EOF
cat .agents/config.yaml
```

Expected output:

```yaml
apiVersion: agent-marketplace/v2
harnesses: [claude]
registries: []
```

This file selects the destination harness but does not select any skills yet. Existing organization and user settings still apply; the practice repository does not bypass their policies.

## 2. Connect the online Marketplace

Register your operator-provided endpoint in this repository. Replace the example hostname before running:

```bash
agent-marketplace install https://marketplace.example.com --scope repo
```

Representative output:

```text
Added Marketplace https://marketplace.example.com to /tmp/marketplace-tutorial.a1B2c3/.agents/config.yaml
```

The CLI verifies the endpoint's identity and saves it under `marketplaces:`. If the same Marketplace is already configured in user or organization scope, the command reports that it is already configured instead. Either result lets you continue.

Notice that connecting a Marketplace does not install its entire catalog.

## 3. Find and inspect the skill

Search only this Marketplace for the skill your operator identified:

```bash
agent-marketplace search review \
  --kind skill \
  --marketplace https://marketplace.example.com
```

Representative result:

```text
MARKETPLACE                      SOURCE         KIND   NAME    DESCRIPTION
https://marketplace.example.com   shared-agents  skill  review  Review code changes
```

The first column may show a configured alias instead of the URL. Confirm that **SOURCE**, **KIND**, and **NAME** match the intended skill. These three fields identify the resource you will install. If the command prints `No resources found.`, confirm the published skill name and your access with the operator before continuing.

Open your organization's Marketplace in a browser and open the matching skill's detail page. Review its instructions, provenance, license, and any reported findings before installing. A catalog listing is not a substitute for reviewing instructions your agent will follow.

## 4. Install the skill into the practice repository

Use the source, kind, and name from the search result, with the same endpoint:

```bash
agent-marketplace install shared-agents/skill/review \
  --marketplace https://marketplace.example.com \
  --scope repo
```

The CLI verifies the resource and prints an installation preview. An abbreviated successful result looks like this; your identity, digests, and acquisition details will differ:

```text
Marketplace install preview:
marketplace: https://marketplace.example.com (https://marketplace.example.com)
instance_id: ...
registry: shared-agents (...)
kind: skill
name: review
installed_name: review
revision: sha256:...
content_digest: sha256:...
...
findings: none
capabilities: none
provenance: ...
Installed review in this repo
```

The command saves an exact content pin under `marketplaces[].resources` in `.agents/config.yaml` and synchronizes the Claude Code output. You do not need a separate `sync` command to install it.

This lesson uses a skill that needs no risk acceptance. If installation stops for findings or active capabilities, do not add a blanket approval flag just to finish the tutorial. Review the preview with your operator; the [hosted Marketplace how-to](../how-to/install-from-a-marketplace.md#_4-review-findings-and-capabilities) covers explicit consent outside this lesson.

## 5. Verify the installed revision and file

Inspect the installation:

```bash
agent-marketplace status
```

Under **MARKETPLACE PINS**, locate the row for your endpoint and confirm:

- **REGISTRY_SLUG** is `shared-agents`;
- **KIND** is `skill`;
- **NAME** and **INSTALLED_NAME** are `review`;
- **LAYER** is `REPO`;
- **STATE** is `current`; and
- **CONTENT_DIGEST** is the `sha256:` digest shown by the install preview.

Check that the generated file is readable:

```bash
test -r .claude/skills/review/SKILL.md && printf 'Claude Code skill is ready\n'
```

Expected output:

```text
Claude Code skill is ready
```

Finally, check for drift:

```bash
agent-marketplace sync --check
```

Expect exit status `0` with no drift verdict. Existing user or organization resources may also appear in the output. If the check reports drift, use the [sync drift guide](../how-to/troubleshoot-sync-drift.md) before continuing.

## 6. Use the skill in Claude Code

Start a new Claude Code session in the practice repository so it discovers `.claude/skills/review/SKILL.md`. Give it this task, substituting your skill name:

```text
Use the review skill to review .agents/config.yaml.
Summarize what it configures and any concerns. Do not modify files.
```

Expect a review of the configuration rather than file edits; the wording depends on your skill and model. Keep changes to the skill itself out of the generated directory—Agent Marketplace manages that output.

You have connected a hosted catalog, found one resource, installed an exact revision, and made its instructions available to Claude Code in a repository.

## Next steps

- [Install from a hosted Marketplace](../how-to/install-from-a-marketplace.md) for aliases, browser resource URLs, explicit risk consent, and name conflicts.
- [Adopt Agent Marketplace in a shared repository](../how-to/adopt-agent-marketplace-in-a-repository.md) when you are ready to apply the workflow to a real project.
- Read [Git registries and hosted Marketplaces](../explanation/git-registries-and-hosted-marketplaces.md) to understand how the two installation sources differ.
