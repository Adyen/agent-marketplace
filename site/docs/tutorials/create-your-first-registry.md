# Create your first registry

In this tutorial you build a small [registry](../glossary.md#registry): a [Git](../glossary.md#git) repository that holds one [skill](../glossary.md#skill). You then point a test project at it and check that `agent-marketplace` installs the skill for Claude Code. Everything happens in temporary folders, so nothing on your computer changes outside them.

## Before you begin

You need:

- Git. Run `git --version` to check that it is installed.
- `agent-marketplace` installed and on your `PATH`. See [Install](../install.md). Running `agent-marketplace --version` should print a version number.

You do not need Claude Code installed. The tutorial only checks that the files appear where Claude Code would read them.

## 1. Start in an empty temporary folder

```bash
cd $(mktemp -d)
```

`mktemp -d` creates an empty folder with a random name, and `cd` moves you into it. The operating system may delete temporary folders later, which is fine here.

## 2. Create the registry repository

```bash
mkdir -p agent-skills
cd agent-skills
git init -b main
```

These commands create a folder named `agent-skills`, move into it, and turn it into a Git repository whose main branch is called `main`. Git prints a line starting with `Initialized empty Git repository`.

If Git answers `unknown switch 'b'`, your Git is older than version 2.28. Run `git init` and then `git checkout -b main` instead.

## 3. Add a skill

A skill is a folder containing a file named `SKILL.md`. The file starts with a short header called front matter: lines of [YAML](../glossary.md#yaml) between two `---` lines. The front matter gives the skill a `name` and a `description`. Everything below it is the instruction text the AI agent reads.

Create the skill with one command:

```bash
mkdir -p skills/hello-world
cat > skills/hello-world/SKILL.md << 'EOF'
---
name: hello-world
description: Greet new contributors.
---

# hello-world

This skill greets new contributors.

## Usage

When someone asks how to start, greet them warmly and point them to the README.
EOF
```

`cat > file << 'EOF'` writes every line up to the closing `EOF` into the file.

The `name` may contain only lowercase letters, digits, and hyphens, and it must start with a letter. `agent-marketplace` rejects a skill whose name breaks that rule.

## 4. Commit the registry

```bash
git add .
git commit -m "Add hello-world skill"
```

`git add .` marks every file in the folder to be saved, and `git commit` saves them as a [commit](../glossary.md#commit), a snapshot of the repository. Git prints a summary such as `1 file changed`.

If Git answers `Please tell me who you are`, it does not know your name yet. Run the two `git config --global` commands it suggests, with your own name and email, and then run `git commit` again.

## 5. Record where the registry is

```bash
REGISTRY_PATH=$(pwd)
echo "$REGISTRY_PATH"
```

`pwd` prints the full path of the folder, and the first line stores it in `REGISTRY_PATH`. The variable exists only in this terminal window. In a new window, go back to the `agent-skills` folder and run these commands again.

## 6. Create a test project that uses the registry

The test project plays the part of someone who installs your skill. It needs a [configuration file](../glossary.md#configuration-file) at `.agents/config.yaml`:

```bash
cd $(mktemp -d)
mkdir -p .agents
cat > .agents/config.yaml << EOF
apiVersion: agent-marketplace/v4
harnesses:
  - claude
sources:
  local:
    directory: ${REGISTRY_PATH}
resources:
  local:
    include:
      - skill/hello-world
EOF
```

What each part of the file does:

- `harnesses` lists the coding tools to install for. Here it is only `claude`, for Claude Code.
- `sources` names a [source](../glossary.md#source) called `local`. `directory` means it reads the registry folder directly from disk.
- `resources` says what to take from the `local` source: the skill named `hello-world`.

This time the heredoc starts with `<< EOF` without quotes, so the shell replaces `${REGISTRY_PATH}` with the real path. Run `cat .agents/config.yaml` and check that the `directory:` line shows the full path to `agent-skills`. If it is empty, `REGISTRY_PATH` was not set in this window. Repeat step 5 and this step.

## 7. Sync and check the result

```bash
agent-marketplace sync
```

[Sync](../glossary.md#sync) reads the configuration and writes the skill into Claude Code's folders. You should see output like this, with your test project's path in the second line:

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → /tmp/tmp.k3J9x2Qp/.claude/.claude-plugin (1 written), /tmp/tmp.k3J9x2Qp/.claude/skills (1 written)
```

`Installed` counts files written: the skill, and a Claude Code manifest that lists installed skills. If your organization's configuration also installs resources on this computer, the numbers are higher and more folders appear.

If you had no user configuration yet, this first sync also creates `~/.agents/config.yaml` with `harnesses: [factory, windsurf]`; the test project's `harnesses:` list still limits this sync to Claude Code.

If `Malformed` is `1`, the skill's front matter has a problem. Run `agent-marketplace sync --verbose` to see the reason, fix `SKILL.md` in the `agent-skills` folder, and run `agent-marketplace sync` again.

Now look at what sync created:

```bash
ls -l .claude/skills/
cat .claude/skills/hello-world/SKILL.md
```

`hello-world` is a [symlink](../glossary.md#symlink), a pointer, into the `agent-marketplace` [cache](../glossary.md#cache) under `~/.cache/agent-marketplace/`. `cat` prints the skill text you wrote in step 3.

Ask `agent-marketplace` what it found:

```bash
agent-marketplace list
```

The `hello-world` row has the status `enabled`.

## What happened

You created a Git repository with one skill in the layout that `agent-marketplace` recognizes, `skills/<name>/SKILL.md`. The test project's configuration pointed a `directory` source at that folder and selected the skill, and `sync` wrote it into the folders Claude Code reads.

A `directory` source is read straight from disk on every sync and has no [lockfile](../glossary.md#lockfile). To share the registry, push it to a Git host and use a `git` source with a `ref` instead, as in [Install your first skill](./install-your-first-skill.md).

## Next steps

- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
- [How to version and release a registry](../how-to/author/version-and-release-a-registry.md)
- [Resource metadata reference](../reference/resource-metadata.md)
- [Registry layout reference](../reference/registry-layout.md)
