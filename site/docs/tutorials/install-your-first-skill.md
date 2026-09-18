# Install your first skill for Claude Code

Install Anthropic's `frontend-design` skill for Claude Code. The skill will be available machine-wide to projects that use your personal Claude Code target.

You will use a public GitHub repository as the registry, save the selection in your user configuration, and verify the generated Claude Code skill.

## Before you begin

You need:

- Claude Code;
- Git; and
- `agent-marketplace` on your `PATH`. Download a published binary from [GitHub Releases](https://github.com/Adyen/agent-marketplace/releases), or follow the [source build instructions](https://github.com/Adyen/agent-marketplace#build-from-source).

Confirm that the CLI is available:

```bash
agent-marketplace --help
```

A skill contains instructions that your coding agent follows. Review third-party skills before installing them. You can inspect the [`frontend-design` source](https://github.com/anthropics/skills/tree/main/skills/frontend-design) used in this tutorial.

Run the tutorial from your home directory. This prevents a repository's own harness settings from overriding your personal target:

```bash
cd ~
```

## 1. Select Claude Code as your harness

Agent Marketplace needs to know which coding harness should receive installed resources. Initialize a personal configuration that targets Claude Code:

```bash
agent-marketplace init --harnesses claude
```

The command creates `~/.agents/config.yaml` and reports its path:

```text
Initialized user personal config: <home>/.agents/config.yaml
```

Check the effective target:

```bash
agent-marketplace status
```

Under **TARGET HARNESS SET**, confirm that the target is `claude` and its source is `USER` before continuing.

## 2. Browse the skill

Use the public HTTPS clone URL to inspect the skill without changing your configuration:

```bash
agent-marketplace list \
  --from https://github.com/anthropics/skills.git \
  --ref main \
  --path skills/frontend-design
```

The results should contain an available skill named `frontend-design`:

```text
REGISTRY  KIND   NAME             TAGS (metadata)  STATUS
skills    skill  frontend-design                   available
```

The `--ref` option selects the `main` branch. The `--path` option limits discovery to the directory that contains this skill.

## 3. Install the skill

Install the skill in user scope:

```bash
agent-marketplace install frontend-design \
  --from https://github.com/anthropics/skills.git \
  --ref main \
  --path skills/frontend-design \
  --scope user
```

The command confirms the installation:

```text
Installed frontend-design user-wide
```

User scope records the selection in `~/.agents/config.yaml` and synchronizes the generated Claude Code files. You do not need to run `agent-marketplace sync` separately.

## 4. Verify the installation

Check the effective configuration and generated resources:

```bash
agent-marketplace status
```

First find the user entry under **EXACT REGISTRY LOCKS**; it shows the declared `main` ref and the exact commit used for this installation. Then find the `frontend-design` row under **SYNCED RESOURCES**. It should report:

- kind: `skill`;
- harness: `claude`;
- source: `USER`;
- registry: `https://github.com/anthropics/skills.git`; and
- ref: `main`.

The **HARNESS TARGETS** section should show Claude Code's machine root as `~/.claude`. The generated skill is available at:

```text
~/.claude/skills/frontend-design/SKILL.md
```

Agent Marketplace normally creates this skill directory as a link to its compiled cache. Manage it through Agent Marketplace rather than editing the generated file.

## 5. Use the skill

Start a new Claude Code session so it discovers the new skill. Then give it a frontend task, for example:

```text
Create a distinctive landing page for a neighborhood film photography club.
Use the frontend-design skill and explain the visual direction before building it.
```

Claude Code can now use the installed guidance when planning and implementing the design.

## What happened

You completed three operations:

1. selected Claude Code as the target harness;
2. added Anthropic's repository and the `frontend-design` selection to your user configuration; and
3. materialized the skill in Claude Code's native user directory.

For reproducible setups, use a stable tag or commit as the registry ref. The [reproducibility and lockfiles explanation](../explanation/reproducibility-and-lockfiles.md) describes how the lock keeps the exact Git acquisition.

## Next steps

- [Install your first skill from an online Marketplace](./install-your-first-marketplace-skill.md) to learn the hosted catalog workflow.
- Learn the everyday [`list`, `install`, `remove`, `sync`, and `update` commands](../reference/cli.md).
- [Add agent-marketplace to a repository](https://github.com/Adyen/agent-marketplace/blob/main/docs/adoption-guide.md) to share selections with a team.
- Read the [configuration reference](../reference/configuration.md) when you need to edit `.agents/config.yaml` directly.

The tutorial follows the repository's `main` branch for an approachable first installation. For reproducible setups, configure a stable tag or commit as the registry ref.
