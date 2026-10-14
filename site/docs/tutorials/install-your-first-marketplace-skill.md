# Install your first skill from an online Marketplace

In this tutorial you find a skill in your organization's hosted [Marketplace](../glossary.md#marketplace), install it for Claude Code, and check the exact revision you received. You work in a throwaway practice repository and save the selection there, so nothing is installed for your whole machine.

The [Git registry tutorial](./install-your-first-skill.md) starts from a Git repository address. This one starts from a searchable online catalog, so you don't need to know which Git repository holds the skill.

## Before you begin

You need:

- `agent-marketplace` on your `PATH` ([installation instructions](../install.md));
- Git and Claude Code; and
- access to your organization's Marketplace from both your browser and the terminal, including any VPN or other access setup your operator requires.

If you rarely use a terminal, read [Terminal basics](../terminal-basics.md) first.

::: warning There is no public Marketplace for this tutorial
`https://marketplace.example.com` is a placeholder, not a working service. Ask your Marketplace operator for the Marketplace address and for a published, approved skill with no security findings that suits a first installation. The examples use a code-review skill named `review` in a registry called `shared-agents`. Replace the address, registry, and skill name with yours in every command.
:::

Check that the program is available:

```bash
agent-marketplace --help
```

You should see a list of commands that includes `install`, `search`, `status`, and `sync`. If the terminal answers `command not found`, go back to [Install](../install.md).

## 1. Prepare a practice repository

Create a temporary folder, move into it, and make it a Git repository:

```bash
cd "$(mktemp -d /tmp/marketplace-tutorial.XXXXXX)"
git init --quiet
pwd
```

The last command prints a path like `/tmp/marketplace-tutorial.a1B2c3`. On macOS the path may start with `/private/tmp`, which is the same folder. Keep this terminal window in that folder for the rest of the tutorial.

Create a project configuration that selects Claude Code for this repository:

```bash
mkdir .agents
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses: [claude]
EOF
cat .agents/config.yaml
```

The last command prints the file:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
```

This file chooses the [harness](../glossary.md#harness) that receives resources, but doesn't select any skills yet. Your personal configuration and any organization configuration still apply; the practice repository doesn't bypass their policies.

## 2. Connect the online Marketplace

Register the Marketplace address in this repository. Replace the example address before you run the command:

```bash
agent-marketplace install https://marketplace.example.com --scope repo
```

You should see something like:

```text
Added Marketplace https://marketplace.example.com to /tmp/marketplace-tutorial.a1B2c3/.agents/config.yaml
```

The command checks the Marketplace's identity and adds it to `.agents/config.yaml` as a [source](../glossary.md#source) named `marketplace`:

```yaml
sources:
  marketplace:
    marketplace: https://marketplace.example.com
```

If the Marketplace is already configured, the command says so and changes nothing. Connecting a Marketplace doesn't install anything from its catalog.

## 3. Find and inspect the skill

Search this Marketplace for the skill your operator named:

```bash
agent-marketplace search review \
  --kind skill \
  --marketplace https://marketplace.example.com
```

A result looks like this:

```text
MARKETPLACE    SOURCE           KIND     NAME      DESCRIPTION
"marketplace"  "shared-agents"  "skill"  "review"  "Review code changes"
```

The first column shows the Marketplace's name in your configuration. Check that `SOURCE`, `KIND`, and `NAME` match the skill you want. Those three values identify the resource you will install. If the command prints `No resources found.`, check the skill name and your access with your operator before you continue.

Open the Marketplace in your browser and go to the skill's page. Read its instructions and license, and check the **Security** tab for findings, before you install it. Your agent will follow these instructions, so read them yourself.

## 4. Install the skill into the practice repository

Build the skill's address from the Marketplace address and the SOURCE, KIND, and NAME from the search result. The pattern is `<marketplace>/r/<source>/<kind>/<name>`:

```bash
agent-marketplace install https://marketplace.example.com/r/shared-agents/skill/review \
  --scope repo \
  --verbose
```

The command checks the exact revision of the skill before it installs anything. `--verbose` prints the full installation preview. An abbreviated successful result looks like this; your identifiers and digests differ:

```text
Installing review · skill

Source: shared-agents

Marketplace install preview:
marketplace: "marketplace" ("https://marketplace.example.com")
instance_id: "..."
registry: "shared-agents" ("...")
kind: "skill"
name: "review"
installed_name: "review"
revision: "sha256:..."
content_digest: "sha256:..."
...
findings: none
capabilities: ["skill"]
...

✓ Installed review
```

The command adds the skill to the `resources` part of `.agents/config.yaml`, records its exact revision in the [lockfile](../glossary.md#lockfile) `.agents/lock.yaml`, and writes the Claude Code files. You don't need to run `sync` separately.

```yaml
resources:
  marketplace:
    include:
      - shared-agents/skill/review
```

This skill has no security findings, so the command installs it without asking. If your resource reports findings instead, stop and review them with your operator; the [hosted Marketplace how-to](../how-to/install-from-a-marketplace.md#_4-review-findings-and-capabilities) covers what consent looks like.

## 5. Check the installed revision and file

Look at the installation:

```bash
agent-marketplace status
```

Under `MARKETPLACE PINS`, find the row for your Marketplace and check that:

- `REGISTRY_SLUG` is `shared-agents`;
- `KIND` is `skill`;
- `NAME` is `review`;
- `LAYER` is `REPO`;
- `STATE` is `current`; and
- `CONTENT_DIGEST` is the `sha256:` value that the install preview showed.

The table is wide. If your terminal wraps the lines, run `agent-marketplace status | less -S` and scroll sideways with the arrow keys.

Check that Claude Code's copy of the skill is readable:

```bash
test -r .claude/skills/review/SKILL.md && printf 'Claude Code skill is ready\n'
```

You should see:

```text
Claude Code skill is ready
```

Finally, check for [drift](../glossary.md#drift), which means differences between the configuration and the files on disk:

```bash
agent-marketplace sync --check
echo $?
```

`echo $?` prints `0`; `sync --check` itself prints nothing. If it prints `1` instead, the [sync drift guide](../how-to/troubleshoot-sync-drift.md) shows how to repair the drift.

## 6. Use the skill in Claude Code

Start a new Claude Code session in the practice repository so it finds `.claude/skills/review/SKILL.md`. Give it this task, with your skill's name:

```text
Use the review skill to review .agents/config.yaml.
Summarize what it configures and any concerns. Do not modify files.
```

You should get a review of the configuration, not file edits. The wording depends on your skill and model. Don't edit the skill inside `.claude/skills/`, because `agent-marketplace` manages that folder.

## What happened

You connected a hosted catalog, found one resource, installed an exact revision, and made its instructions available to Claude Code in a repository.

## Next steps

- [Install from a hosted Marketplace](../how-to/install-from-a-marketplace.md) for aliases, browser resource URLs, explicit risk consent, and name conflicts.
- [Adopt agent-marketplace in a shared repository](../how-to/adopt-agent-marketplace-in-a-repository.md) when you are ready to use this workflow in a real project.
- Read [Git registries and hosted Marketplaces](../explanation/git-registries-and-hosted-marketplaces.md) to understand how the two kinds of source differ.
