# How to add a resource to a registry

Add a new skill or command to a Git registry. Subagent, hook, and MCP files follow source-harness-specific native formats; see the [registry layout reference](../../reference/registry-layout.md).

## Prerequisites

- A Git registry in a supported layout.
- `agent-marketplace` on your `PATH`.

## 1. Choose the correct directory

Resource layouts are source-harness-specific. A portable skill is a directory containing `SKILL.md`:

| Kind | Directory |
|---|---|
| skill | `skills/<name>/SKILL.md` |

## 2. Author the resource

Public `SKILL.md` frontmatter is required and uses top-level `name`, `description`, and optional `license`; `metadata` owns `tags` and `requires`. For a skill:

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

For a Claude source harness, commands are flat Markdown files under `commands/`:

```bash
mkdir -p commands
cat > commands/release.md << 'EOF'
# release

Run the release checklist.
EOF
```

Then set `source_harness: claude` on the registry entry.

The `x-agent-marketplace-kind: command` marker belongs to Devin and Codex converted-command layouts, where a command is written as a skill-shaped `SKILL.md`. It is not the universal command format.

## 3. Validate locally

```bash
agent-marketplace convert \
  --from claude \
  --to factory \
  --out /tmp/code-review-factory \
  skills/code-review
```

If the conversion emits errors, fix the source before committing.

## 4. Commit and tag

```bash
git add skills/code-review
git commit -m "Add code-review skill"
git tag v1.5.0
```

## 5. Update consumer references

Consumers who pin `v1.5.0` will pick up the new resource. Consumers on a mutable ref such as `main` will see it on the next `agent-marketplace update`.

## Troubleshooting

- **`Malformed resource`**: Check the [diagnostics reference](../../reference/diagnostics.md) for the emitted code and fix the corresponding field.
- **Resource does not appear in `list`**: Verify the directory is under an ingestion root declared in the consumer's registry entry.
