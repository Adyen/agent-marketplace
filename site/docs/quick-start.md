# Quick start

If `agent-marketplace` is not installed yet, see [Install](./install.md). Then open a terminal in a project you work on with a coding agent:

```bash
cd ~/code/my-project
```

In about five minutes you will point the project at a public skill registry, select a skill in a v4 configuration, and commit the files that let teammates reproduce the same setup with one command.

## Launch

Run the binary with no arguments:

```bash
agent-marketplace
```

This quick start uses **Claude Code** so every path and screenshot matches what you see. The same flow works for any of the seven supported harnesses; the [harnesses and output paths reference](./reference/harnesses-and-output-paths.md) lists where each one puts its files, and [How to configure multiple harnesses](./how-to/configure-multiple-harnesses.md) covers switching later.

On first launch there is no personal configuration yet, so the UI asks which coding harnesses it should write into. Harnesses it detects on your machine are pre-selected:

![First-run modal listing the seven harnesses, with Claude Code pre-selected](./images/quick-start/first-run-harness-modal.png)

Make sure only **Claude Code** is checked: move with `j`/`k`, toggle with `space`, and press `enter`. The choice is saved to `~/.agents/config.yaml`.

Press `q` to leave the UI for a moment.

## Point the project at a registry

A registry is a Git repository that holds skills, commands, subagents, hooks, or MCP servers. Declare it as a named v4 `git` source and select resources through the matching resource group ([sources and resources](./explanation/sources-and-resources.md) explains how the two fit together):

```bash
mkdir -p .agents
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses:
  - claude
sources:
  skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
resources:
  skills:
    include:
      - skill/frontend-design
EOF
```

Run the first synchronization:

```bash
agent-marketplace sync
```

The lock records the exact commit resolved for `main`, and the skill is materialized in Claude Code's native layout. For a reproducible team setup, replace `main` with a stable tag or commit before committing the files.

## Browse the configured source

Launch the UI again:

```bash
agent-marketplace
```

Press `2` for the **Registry** tab. Every resource the source offers is listed, and the right pane describes whichever one is selected. The v4 configuration remains the source of truth; edit its `sources` and `resources` maps directly when changing selections.

Before enabling a skill you can read it. Press `enter` on any row to open its files; `SKILL.md` is selected first, and `j`/`k` move through the rest. `esc` returns to the list.

![File browser for the pdf skill: a file tree on the left with SKILL.md selected and its rendered contents on the right](./images/quick-start/skill-file-browser.png)

Press `q` to quit. If the configuration has pending changes, run `agent-marketplace sync` before using the generated files.

## Check what landed

```bash
agent-marketplace status
```

The relevant sections look like this:

```text
TARGET HARNESS SET
TARGET HARNESSES  SOURCE
claude            REPO

EXACT REGISTRY LOCKS
SOURCE  REGISTRY                              REF   COMMIT                                         SNAPSHOT
REPO    https://github.com/anthropics/skills  main  sha1:34040c9c568585f6929bedeaad110ad08f079624  available

SYNCED RESOURCES
KIND   NAME             HARNESS  SOURCE  ...  REGISTRY                                  REF   CACHE
skill  frontend-design  claude   REPO    ...  https://github.com/anthropics/skills.git  main  locked: sha1:34040c9c...

DRIFT
In sync
```

The generated output is on disk:

```bash
ls .claude/skills/
```

```text
frontend-design
```

Start a new Claude Code session in the project and it picks up the skill.

## Commit the config, not the output

Commit the v4 configuration and repository lock. Ignore the generated Claude Code output, which is recreated from the lock on every sync (other harnesses write to different directories; see the [output paths reference](./reference/harnesses-and-output-paths.md)):

```bash
echo '.claude/skills/' >> .gitignore
git add .agents/config.yaml .agents/lock.yaml .gitignore
git commit -m "Add agent-marketplace skills"
```

A teammate who clones the project runs one command to get the identical skill at the identical commit:

```bash
agent-marketplace sync
```

On a fresh CI runner, run `agent-marketplace sync --locked` to download the exact snapshots and generate output without rewriting config or locks. Then run `agent-marketplace sync --check` to check for drift. See [keeping resources reproducible](./how-to/keep-resources-reproducible.md).

## Where next

- [Configure multiple harnesses](./how-to/configure-multiple-harnesses.md): add Cursor, Codex, Devin, or others alongside Claude Code.
- [Install your first skill for Claude Code](./tutorials/install-your-first-skill.md): the same flow, CLI only, installed user-wide instead of per project.
- [Install from a hosted Marketplace](./how-to/install-from-a-marketplace.md): register an endpoint, or install from a browser URL using an editable config.
- [User journeys](./explanation/user-journeys.md): share your setup with a team, keep it updated, or remove resources and recover from errors.
- [TUI reference](./reference/tui.md): every key binding and modal action.
- [Command-line reference](./reference/cli.md): `list`, `install`, `remove`, `sync`, `update`, `status`, and every flag.
- [Sources and resources](./explanation/sources-and-resources.md): what the `sources` and `resources` maps each do.
- [Repository configuration reference](./reference/configuration.md): every v4 source, resource-group, harness, and materialization field.
