# How to add a resource to a registry

Add a skill or command to a Git [registry](../../glossary.md#registry), check it locally, and publish it. For subagents, hooks, and MCP servers, the [registry layout reference](../../reference/registry-layout.md) lists each harness's file formats and paths.

## Prerequisites

- A Git registry in one of the layouts in the [registry layout reference](../../reference/registry-layout.md).
- `agent-marketplace` on your `PATH`.
- Permission to push to the registry repository.

## 1. Choose the directory

`agent-marketplace` reads a registry in the native layout of one [harness](../../glossary.md#harness), its source harness. A registry with `skills/` or `commands/` at its root uses the Claude Code layout, which these examples use:

| Kind | Path in a Claude-layout registry |
|---|---|
| skill | `skills/<name>/SKILL.md`, plus any support files in the same folder |
| command | `commands/<name>.md` |

Other layouts put these resources elsewhere. For example, Codex reads skills from `.agents/skills/<name>/`, and Windsurf reads commands from `workflows/<name>.md`. See the [registry layout reference](../../reference/registry-layout.md).

## 2. Write the resource

A skill's `SKILL.md` starts with YAML front matter. Use top-level `name` and `description`, and optionally `license`. Put `tags` and `requires` under `metadata`:

```bash
mkdir -p skills/code-review
cat > skills/code-review/SKILL.md << 'EOF'
---
name: code-review
description: Review code changes and suggest improvements.
license: Apache-2.0
metadata:
  tags:
    - review
    - quality
---

# code-review

Review code changes and suggest improvements.
EOF
```

Both commands succeed silently and leave the file at `skills/code-review/SKILL.md`. Use a name with only lowercase letters, digits, and hyphens, starting with a letter, as the [resource metadata reference](../../reference/resource-metadata.md) requires.

In the Claude Code layout, a command is a Markdown file under `commands/`. Its name comes from the file name:

```bash
mkdir -p commands
cat > commands/release.md << 'EOF'
# release

Run the release checklist.
EOF
```

These commands print nothing and create `commands/release.md`. This guide releases both `skills/code-review` and `commands/release`.

Consumers can set `source_harness: claude` on their source to state the layout explicitly instead of relying on detection.

Devin and Codex have no separate command format. In those layouts, a command is a skill-shaped `SKILL.md` with the top-level marker `x-agent-marketplace-kind: command`. Other layouts do not use the marker.

## 3. Check it locally

From the registry root, list what `agent-marketplace` discovers:

```bash
agent-marketplace list --from .
```

Typical output:

```text
REGISTRY  KIND     NAME         TAGS (metadata)  STATUS
.         skill    code-review  review,quality   available
.         command  release                       available
```

The new resource shows `available`. To check every harness in CI, see [How to validate a registry in CI](./validate-a-registry-in-ci.md).

## 4. Commit, tag, and push

```bash
git add skills/code-review commands/release.md
git commit -m "Add code-review skill and release command"
git tag v1.5.0
git push origin main v1.5.0
```

`git add` and `git tag` succeed silently. The commit prints a summary such as `[main 1a2b3c4] Add code-review skill and release command` followed by the changed-file count. The push prints the updated remote refs, for example `main -> main` and `v1.5.0 -> v1.5.0`.

## 5. Tell consumers how to get it

A consumer receives the new resource only if their configuration selects it: by name, by a tag it carries, or with `all: true`.

- A consumer whose source uses `ref: v1.5.0` gets it on the next `agent-marketplace sync`.
- A consumer on a branch such as `main` gets it after `agent-marketplace update`, which moves their [lockfile](../../glossary.md#lockfile) to the newest commit. A plain `sync` keeps the commit already recorded in the lockfile.
- A consumer on an older tag has to change the ref. See [How to version and release a registry](./version-and-release-a-registry.md).

## Troubleshooting

- `list --from .` shows the resource with a status starting with `malformed:`, or a consumer's sync counts it as `Malformed`: the status (or `agent-marketplace sync --verbose` in the consumer project) names a diagnostic code such as `ingest.claude.load-skill-failed` and the reason. See the [diagnostics reference](../../reference/diagnostics.md).
- The resource does not appear in `list`: check that it sits inside the `path` the consumer's source declares, and in a directory that the registry's layout reads.
