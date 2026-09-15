# Create your first registry

In this tutorial, you will create a Git registry from scratch, add one skill with valid `SKILL.md` frontmatter, and verify that `agent-marketplace` can install it.

## Before you begin

You need:

- Git.
- `agent-marketplace` on your `PATH`.
- A temporary directory for the tutorial.

Run the tutorial from an empty temporary directory:

```bash
cd $(mktemp -d)
```

## 1. Create the registry repository

```bash
mkdir -p agent-skills
cd agent-skills
git init -b main
```

## 2. Add a skill with frontmatter

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

## 3. Commit the registry

```bash
git add .
git commit -m "Add hello-world skill"
```

## 4. Capture the absolute registry path

Record the absolute path before leaving the registry directory:

```bash
REGISTRY_PATH=$(pwd)
```

## 5. Create the consumer directory and configuration

```bash
cd $(mktemp -d)
mkdir -p .agents
cat > .agents/config.yaml << EOF
apiVersion: agent-marketplace/v2
harnesses:
  - claude
registries:
  - path: ${REGISTRY_PATH}
    skills:
      include:
        - hello-world
EOF
```

## 6. Sync and verify

```bash
agent-marketplace sync
```

Expected output:

```text
Installed 1  Removed 0  Malformed 0  Errors 0
```

Verify the skill is in the Claude Code directory:

```bash
ls .claude/skills/hello-world/SKILL.md
```

The file should exist and contain the skill body.

## What happened

You created a Git repository with one skill, pointed a consumer configuration at its absolute path, and the CLI materialized the skill into the Claude Code layout.

## Next steps

- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
- [Resource metadata reference](../reference/resource-metadata.md)
