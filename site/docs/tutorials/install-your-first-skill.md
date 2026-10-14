# Install your first skill for Claude Code

In this tutorial you install Anthropic's `frontend-design` skill for Claude Code. You install it for your user, so it is available in every project where Claude Code uses your personal setup.

You will use a public GitHub repository as the [registry](../glossary.md#registry), save the selection in your personal configuration file, and check the skill that `agent-marketplace` writes for Claude Code. Everything happens on the command line.

## Before you begin

You need:

- Claude Code.
- Git. Run `git --version` to check. [Terminal basics](../terminal-basics.md#install-git) explains how to install it.
- `agent-marketplace` on your `PATH`. [Install](../install.md) explains how.

If you rarely use a terminal, read [Terminal basics](../terminal-basics.md) first.

Check that the program is available:

```bash
agent-marketplace --help
```

You should see a list of commands that includes `init`, `list`, `status`, and `sync`. If the terminal answers `command not found`, the program isn't on your `PATH` yet; go back to [Install](../install.md).

A [skill](../glossary.md#skill) is a set of instructions that your coding agent follows. Review third-party skills before you install them. You can read the [`frontend-design` source](https://github.com/anthropics/skills/tree/main/skills/frontend-design) used in this tutorial.

Run the tutorial from your home folder. A project folder can have its own harness settings that override your personal ones, and your home folder has none:

```bash
cd ~
```

## 1. Select Claude Code as your harness

`agent-marketplace` needs to know which coding tool, or [harness](../glossary.md#harness), should receive the resources you install. Create a personal configuration that targets Claude Code:

```bash
agent-marketplace init --harnesses claude
```

The command creates `~/.agents/config.yaml` and prints its path:

```text
Initialized user personal config: <home>/.agents/config.yaml
```

If it prints `User personal config already initialized.` instead, you already have a personal configuration. Step 3 replaces that file, and the `sync` after it removes anything your old configuration installed into your personal harness folders. Copy the configuration and its lockfile first (the second copy is skipped if you have no lockfile):

```bash
cp ~/.agents/config.yaml ~/.agents/config.yaml.bak
cp ~/.agents/lock.yaml ~/.agents/lock.yaml.bak 2>/dev/null
```

To undo the tutorial afterwards, copy the two `.bak` files back over `~/.agents/` and run `agent-marketplace sync`; [Back up and restore machine resources](../how-to/back-up-and-restore-machine-resources.md) covers the fuller procedure.

Check which harness is selected:

```bash
agent-marketplace status
```

Under `TARGET HARNESS SET`, confirm that the target is `claude` and its source is `USER`:

```text
TARGET HARNESS SET
TARGET HARNESSES  SOURCE
claude            USER
```

## 2. Browse the skill

List what the registry offers without changing your configuration. The command uses the repository's public HTTPS address:

```bash
agent-marketplace list \
  --from https://github.com/anthropics/skills.git \
  --ref main \
  --path skills/frontend-design
```

`--ref main` reads the `main` branch. `--path` limits the search to the folder that holds this skill. The command downloads the repository into a local [cache](../glossary.md#cache) the first time, which can take a few seconds. The result should contain an available skill named `frontend-design`:

```text
REGISTRY  KIND   NAME             TAGS (metadata)  STATUS
skills    skill  frontend-design                   available
```

If the command fails with an error about `git`, check that Git is installed and that you can reach GitHub.

## 3. Select the skill in your configuration

Replace your personal configuration with one that declares the Git source and asks for the skill. Copy the whole block, including the final `EOF` line:

```bash
cat > ~/.agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses:
  - claude
sources:
  anthropic-skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
resources:
  anthropic-skills:
    include:
      - skill/frontend-design
EOF
```

`sources` gives the registry the name `anthropic-skills`. `resources` asks for one skill from that source.

Write the skill into Claude Code's folders:

```bash
agent-marketplace sync
```

You should see:

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → ~/.claude/.claude-plugin (1 written), ~/.claude/skills (1 written)
```

"Installed" counts files written: the skill, and a Claude Code manifest that lists installed skills. If `Errors` is not `0`, read the lines below the summary. They usually name the setting or the registry that failed.

## 4. Check the installation

```bash
agent-marketplace status
```

Look at these tables in the output:

- `EXACT REGISTRY LOCKS` has a `USER` row for `https://github.com/anthropics/skills`. It shows the `main` ref and the exact [commit](../glossary.md#commit) this installation used.
- `SYNCED RESOURCES` has a `frontend-design` row with kind `skill`, harness `claude`, source `USER`, registry `https://github.com/anthropics/skills.git`, and ref `main`.
- `HARNESS TARGETS` shows Claude Code's machine root as `default=<home>/.claude`.
- `DRIFT` says `In sync`.

The skill is now at:

```text
~/.claude/skills/frontend-design/SKILL.md
```

`agent-marketplace` normally creates this skill folder as a [symlink](../glossary.md#symlink) (a shortcut) to its cache. Don't edit the file there. Change the configuration and run `sync` instead.

## 5. Use the skill

Start a new Claude Code session so it finds the new skill. Then give it a frontend task, for example:

```text
Create a distinctive landing page for a neighborhood film photography club.
Use the frontend-design skill and explain the visual direction before building it.
```

Claude Code follows the skill's guidance when it plans and builds the design.

## What happened

You did three things:

1. selected Claude Code as the target harness;
2. added Anthropic's repository and the `frontend-design` selection to your personal configuration; and
3. ran `sync`, which wrote the skill into Claude Code's personal folder and recorded the commit in `~/.agents/lock.yaml`.

This tutorial follows the repository's `main` branch. The [lockfile](../glossary.md#lockfile) keeps you on the recorded commit until you run `agent-marketplace update`; the [reproducibility and lockfiles explanation](../explanation/reproducibility-and-lockfiles.md) describes how that works and when to pin a tag or commit instead.

## Next steps

- [Install your first skill from an online Marketplace](./install-your-first-marketplace-skill.md) to learn the hosted catalog workflow.
- Learn the everyday [`list`, `install`, `remove`, `sync`, and `update` commands](../reference/cli.md).
- [Adopt agent-marketplace in a repository](../how-to/adopt-agent-marketplace-in-a-repository.md) to share selections with a team.
- Read the [configuration reference](../reference/configuration.md) when you need to edit `.agents/config.yaml` directly.
