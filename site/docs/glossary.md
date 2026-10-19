# Glossary

Definitions of the terms these docs use. Each entry links to the full details.

## Cache

The copies of registries that `agent-marketplace` downloads to your computer, stored under `~/.cache/agent-marketplace/`. Installed skills are [symlinks](#symlink) into the cache, so deleting it breaks them until the next [sync](#sync) downloads everything again over the network. To free space safely, run `agent-marketplace gc`. See [Clean the cache safely](./how-to/clean-the-cache-safely.md).

## Collection

A named list of [resources](#resource) that you install in one step, such as an onboarding bundle for a team. Registry authors publish collections as `collections/<name>/COLLECTION.yaml`. Installing a collection selects all of its members, and you can still remove single members afterwards. See the [collections reference](./reference/collections.md).

## Command

A saved prompt that you run by name inside your [harness](#harness), often as a slash command such as `/deploy-service`. Harnesses without a separate command type, such as Codex and Devin, receive commands as [skills](#skill).

## Command line

Typing commands into a [terminal](#terminal), such as `agent-marketplace sync`, instead of using a graphical app. The [command-line reference](./reference/cli.md) lists every `agent-marketplace` command. You will also see it called the CLI (command-line interface).

## Commit

A saved snapshot of a [Git](#git) repository at one point in time. Each commit has a long identifier such as `34040c9c568585f6929bedeaad110ad08f079624`. The [lockfile](#lockfile) records exact commits so that everyone installs the same content.

## Configuration file

A text file in [YAML](#yaml) format that says which [harnesses](#harness) to write for, where resources come from, and which resources to install. The ones you are most likely to edit are:

| File | Who it is for |
|---|---|
| `~/.agents/config.yaml` | You, in every project on this computer |
| `.agents/config.yaml` in a project | Everyone who works on the project. Commit it with the project. |
| `.agents/config.local.yaml` in a project | You, in that one project. Keep it out of Git. |

See the [repository configuration reference](./reference/configuration.md).

## Configuration layer

One of the four [configuration files](#configuration-file) that `agent-marketplace` reads and combines on every run: organization (`/etc/agent-marketplace/config.yaml`, managed by your IT or platform team), user, repository, and personal-local. When layers disagree about a resource, the more specific layer usually wins. Resources marked `required: true`, typically by an organization, cannot be excluded by any layer. See [configuration layers](./reference/configuration-layers.md).

## Diagnostic

A warning or error message with a stable code, such as `v4.undefined-source` or `lock.version-deprecated`. The code stays the same between releases, so you can search for it with the search box at the top of this site. The [diagnostics reference](./reference/diagnostics.md) explains how the codes are named.

## Drift

A difference between what your configuration and [lockfile](#lockfile) describe and what is actually on disk. For example, someone added a skill to the configuration but nobody has run [sync](#sync) yet. `agent-marketplace sync --check` reports drift and exits with status `1`, and `agent-marketplace sync` fixes it. See [Troubleshoot sync drift](./how-to/troubleshoot-sync-drift.md).

## Git

A tool that tracks changes to files and shares them between people. Most [registries](#registry) are Git repositories, and `agent-marketplace` runs the `git` program to download them, so Git must be installed.

## Harness

The AI coding tool that uses the resources: Claude Code, Codex, Cursor, Devin, Factory Droid, Gemini CLI, or Windsurf. Each harness looks for files in its own folders and formats, for example `.claude/skills/` for Claude Code. You choose which harnesses `agent-marketplace` writes for with a `harnesses:` list in a configuration file, or with the `H` key in the [TUI](#tui). See [harnesses and output paths](./reference/harnesses-and-output-paths.md).

## Hook

A shell command that your [harness](#harness) runs automatically when something happens, for example before a tool runs or when a session starts. `agent-marketplace` adds hook entries to the file where the harness reads its hooks.

## Lockfile

A generated file, `lock.yaml`, that records the exact [commit](#commit) and a fingerprint of the content for each Git or Marketplace [source](#source). Everyone who runs [sync](#sync) gets identical files, even after a branch moves on. Commit `.agents/lock.yaml` together with `.agents/config.yaml`, and do not edit it by hand. `agent-marketplace update` moves it forward on purpose. See [Reproducibility and lockfiles](./explanation/reproducibility-and-lockfiles.md).

## Locator

The text that names one resource. In a configuration file it is `kind/name`, for example `skill/frontend-design` or `mcp/issue-tracker`. A [Marketplace](#marketplace) locator also names the Marketplace and registry, for example `team:shared-agents/skill/review`. See [Marketplace resource locators](./reference/marketplace-resource-locators.md).

## Marketplace

A web service, run by your organization, that collects resources from many [registries](#registry), checks them for security problems, and lets people search, read, and install them. You can browse it in a web browser, or use it from the terminal with `agent-marketplace search` and `agent-marketplace install`. See [Install from a hosted Marketplace](./how-to/install-from-a-marketplace.md).

## Materialize

The word these docs use for writing resources into your [harness's](#harness) folders in the format that harness expects. When `agent-marketplace` materializes a skill for Claude Code, it creates `.claude/skills/<name>/`. [Sync](#sync) is the command that does it.

## MCP server

A program that gives your AI agent extra tools or data through the Model Context Protocol (MCP), for example access to an issue tracker or a database. `agent-marketplace` adds the server's entry to your [harness's](#harness) MCP settings, which tells the harness how to start or reach the server. It leaves the entries you added yourself in place.

## Ownership state

Records that `agent-marketplace` keeps about which entries it wrote into settings files it shares with you, such as Devin's `mcp_config.json` or Codex's `config.toml`. They let it update or remove only its own entries. The records live in `~/.local/state/agent-marketplace/` (or `$XDG_STATE_HOME/agent-marketplace/` when that variable is set). Do not delete them. See [Shared configuration ownership](./explanation/shared-configuration-ownership.md).

## Prerequisite

Another resource that a resource needs to work, declared in its front matter under `metadata.requires`. An unmet prerequisite produces an advisory warning with the install command that fixes it; it never blocks the install. Only `sync --strict` (including `sync --strict --check`) fails on an unmet prerequisite. See [Declare resource prerequisites](./how-to/author/declare-resource-prerequisites.md) and the [resource prerequisites reference](./reference/resource-prerequisites.md).

## Ref

The Git branch, tag, or commit that a Git [source](#source) points at, written as `ref:` in the configuration, for example `main` or `v1.2.0`. The first [sync](#sync) turns the ref into one exact [commit](#commit) and records it in the [lockfile](#lockfile).

## Registry

A place that holds resources: a [Git](#git) repository or a folder on your computer. Anyone can create one. A team might keep its shared skills in a registry, and a [Marketplace](#marketplace) can index many registries. See [Create your first registry](./tutorials/create-your-first-registry.md).

## Repository

A project folder tracked by [Git](#git), together with its history. In these docs, "repository scope" means the project you are working in. Its shared configuration is `.agents/config.yaml` at the top of the project.

## Resource

Something `agent-marketplace` installs for your [harness](#harness). There are five kinds: [skill](#skill), [command](#command), [subagent](#subagent), [hook](#hook), and [MCP server](#mcp-server). A [collection](#collection) groups several resources together.

## Resource group

An entry under `resources:` in a configuration file. It has the same name as a [source](#source) and says what to take from that source: named resources with `include`, resources to skip with `exclude`, everything with a [tag](#tag) using `tags`, or everything with `all: true`. See [Sources and resources](./explanation/sources-and-resources.md).

## Scope

Where a change applies and where the files are written.

| Scope | Applies to | Files are written to |
|---|---|---|
| `user` | You, in every project on this computer | Your home folder, for example `~/.claude/` |
| `repo` | Everyone working on the project | The project, for example `.claude/` |
| `local` | You, in one project only | The project, but the configuration stays out of Git |

Commands such as `install` and `remove` take a `--scope` option. An organization scope also exists, managed by administrators. See [Scopes, selection, and precedence](./explanation/scopes-selection-and-precedence.md).

## Skill

A folder with a `SKILL.md` file that teaches the AI agent how to do a specific task, such as reviewing a pull request or designing a web page. It can include extra files such as scripts or templates. Skills follow the [Agent Skills specification](https://agentskills.io/specification).

## Source

A named entry under `sources:` in a configuration file. It says where resources come from: a Git repository (`git`), a folder on your computer (`directory`), or a [Marketplace](#marketplace) (`marketplace`). A source on its own installs nothing. The [resource group](#resource-group) with the same name picks what to install from it. See [Sources and resources](./explanation/sources-and-resources.md).

## Subagent

A helper agent with its own instructions that the main AI agent can hand work to. Not every [harness](#harness) supports subagents. Windsurf, for example, has none, and `agent-marketplace` reports a [diagnostic](#diagnostic) instead of writing a file.

## Symlink

Short for symbolic link: a small file that points to another file or folder, like a shortcut. By default `agent-marketplace` installs skills as symlinks into its [cache](#cache). If your tools do not follow symlinks, it can write regular copies instead. See [Write regular files instead of symlinks](./how-to/write-regular-files-instead-of-symlinks.md).

## Sync

The `agent-marketplace sync` command. It downloads what your configuration asks for, updates the [lockfile](#lockfile) if needed, and writes, updates, or removes files in your [harness's](#harness) folders so they match the configuration. Run it after you change a configuration file, or after you pull changes from teammates.

## Tag

A label that a registry author puts on a resource, such as `k8s` or `security`. You can install everything with a tag at once, with `tags: [k8s]` in a [resource group](#resource-group) or with `agent-marketplace install --tag k8s`. See [Declare tags and a license](./how-to/author/declare-tags-and-licenses.md).

## Terminal

An app where you type commands instead of clicking, such as Terminal on macOS.

## TUI

Short for terminal user interface: a full-screen, keyboard-driven screen inside the [terminal](#terminal). Running `agent-marketplace` with no command opens it, and so does `agent-marketplace tui`. In the TUI you can browse registries, read resources before installing them, and choose harnesses. See the [TUI reference](./reference/tui.md).

## YAML

The plain-text format of the configuration files. Indentation with spaces decides which setting belongs to which, and tabs are not allowed. A line starting with `#` is a comment.
