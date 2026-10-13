# How to adopt agent-marketplace in a repository

Add `agent-marketplace` to a repository so everyone on the team gets the same skills and other resources.

## Prerequisites

- `agent-marketplace` is installed and on your `PATH` (see [Install](../install.md)). Everyone who syncs the repository, including CI, needs version v4.0.9 or newer, because older versions cannot read the lockfile this guide creates.
- You have the URL of a Git [registry](../glossary.md#registry) that contains the resources you want.
- You know which [harnesses](../glossary.md#harness) the project uses.
- A terminal open at the repository's root folder.

## 1. Create the repository configuration

Create the `.agents` folder:

```bash
mkdir -p .agents
```

`mkdir -p` prints nothing on success.

Create `.agents/config.yaml` with a text editor and add a minimal configuration:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v4
harnesses:
  - claude
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
resources:
  shared:
    include:
      - skill/code-review
```

Replace `https://github.com/example-org/agent-resources.git` with a registry you control or a public example, `v1.4.0` with a tag or branch in that registry, and `skill/code-review` with the resources you want. The `harnesses` list in this file applies to everyone who works in the repository and takes precedence over each person's own list. A teammate's personal resources then go only to harnesses that are in both lists; see [configure multiple harnesses](./configure-multiple-harnesses.md).

## 2. Sync and verify

```bash
agent-marketplace sync
```

Sync resolves the tag to an exact [commit](../glossary.md#commit), writes the lock, and puts the skill into the Claude Code folders. With the configuration above, the output looks like this:

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → /path/to/repo/.claude/.claude-plugin (1 written), /path/to/repo/.claude/skills (1 written)
```

`Installed` counts the files and entries sync wrote, so it can be higher than the number of resources. For Claude Code, sync writes the skill under `.claude/skills/` and lists it in `.claude/.claude-plugin/plugin.json`.

Check that the lock was created:

```bash
ls .agents/
```

```text
config.yaml  lock.yaml
```

`.agents/lock.yaml` records the exact commit that `v1.4.0` resolved to.

::: tip
If `~/.agents/config.yaml` does not exist yet, the first `sync` also creates it, as a v4 file with the default harness list. Existing unsupported roots or fragments refuse before new acquisition or writes; [convert them with an older released migrator before upgrading](./migrate-configuration.md). The repository's `harnesses` list still decides where this repository's output goes.
:::

## 3. Ignore generated harness output

Sync recreates the harness folders from the configuration and the lock, so most teams keep them out of Git. Add them to `.gitignore`. The exact paths depend on your harnesses (see [harnesses and output paths](../reference/harnesses-and-output-paths.md)). For Claude Code:

```bash
printf '%s\n' '.claude/skills/' '.claude/.claude-plugin/' >> .gitignore
```

This appends the two lines to `.gitignore` and prints nothing.

## 4. Commit the configuration

```bash
git add .agents/config.yaml .agents/lock.yaml .gitignore
git commit -m "Add agent-marketplace configuration"
```

`git add` prints nothing; `git commit` prints its usual summary, for example `[main 1a2b3c4] Add agent-marketplace configuration` followed by the file count.

A teammate who clones the repository runs `agent-marketplace sync` and gets the same resources at the same commit. To check the setup in CI, see [how to keep resources reproducible](./keep-resources-reproducible.md).

## Troubleshooting

- If sync reports `no machine-wide target harness set configured`, add `harnesses` to `.agents/config.yaml` or to `~/.agents/config.yaml`.
- If the `ref` does not resolve, check that the tag or branch exists in the registry and that you can clone it. For private registries, see [configure private Git registries](./configure-private-git-registries.md).
- If a teammate or CI job fails with `lock.invalid-document`, check its version with `agent-marketplace --version`, which prints the installed version such as `v4.1.3`. Versions older than v4.0.9 cannot read the lock; upgrade them.
