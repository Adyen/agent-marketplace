# Quick start

`agent-marketplace` installs shared skills, commands, subagents, hooks, and MCP servers from Git into the folders your coding tools read. This page installs the CLI and adds the `agent-marketplace` skill to your project, so your coding agent can help you with the rest.

You need Git, Go 1.27.3 or newer, and a project folder that is a Git repository.

## Install the CLI

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
agent-marketplace --version
```

If the terminal answers `command not found`, add Go's install folder to your `PATH`: `$(go env GOBIN)` if set, otherwise `$(go env GOPATH)/bin` (usually `~/go/bin`). For release binaries and other options, see [more install options](./install.md).

## Launch

Move into your project folder and run the program with no arguments:

```bash
cd ~/code/my-project
agent-marketplace
```

This opens the terminal UI (TUI), a full-screen interface that you control with the keyboard.

On first launch you have no personal configuration yet, so the TUI asks which [harnesses](./glossary.md#harness) it should install resources into. Harnesses it finds on your computer are already checked:

![First-run modal listing the eight harnesses, with Claude Code pre-selected](./images/quick-start/first-run-harness-modal.png)

Move with the arrow keys, check or uncheck a harness with `space`, and press `enter` to save. The TUI saves your choice to `~/.agents/config.yaml`, your personal configuration file.

If the TUI opens without asking, you already have a personal configuration. Press `H` to see or change its harness list.

Press `q` to leave the TUI.

## Install the agent-marketplace skill

From your project folder, run:

```bash
agent-marketplace install agent-marketplace --from https://github.com/Adyen/agent-marketplace --path skills --scope repo
```

This adds the `skills` folder of the `Adyen/agent-marketplace` repository as a source in `.agents/config.yaml`, installs the `agent-marketplace` skill into each harness you selected, and records the exact commit in `.agents/lock.yaml`. Start a new agent session and ask it to set up more resources for you.

Commit `.agents/config.yaml` and `.agents/lock.yaml`. Teammates then run `agent-marketplace sync` to get the same skill at the same commit.

## Where next

- [Install your first skill for Claude Code](./tutorials/install-your-first-skill.md): a step-by-step walkthrough.
- [Configure multiple harnesses](./how-to/configure-multiple-harnesses.md): choose which coding tools receive resources.
- [TUI reference](./reference/tui.md): browse and install resources with `agent-marketplace`.
- [Command-line reference](./reference/cli.md): every command and flag.
