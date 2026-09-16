# How to adopt agent-marketplace in a repository

Add `agent-marketplace` to a repository and share the same skills and resources with everyone on the team.

## Prerequisites

- `agent-marketplace` is on your `PATH`.
- You have a Git registry URL that contains the resources you want.
- You know which coding harnesses the project uses.

## 1. Initialize the repository configuration

Create the repository configuration directory:

```bash
mkdir -p .agents
```

Write a minimal committed configuration:

```yaml
# .agents/config.yaml
apiVersion: agent-marketplace/v2
harnesses:
  - claude
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
    skills:
      include:
        - code-review
```

Replace `https://github.com/example-org/agent-resources.git` with a registry you control or a public example.

## 2. Synchronize and verify

```bash
agent-marketplace sync
```

Expected output:

```text
Installed 1  Removed 0  Malformed 0  Errors 0
```

Verify the lock was generated:

```bash
ls .agents/
```

```text
config.yaml  lock.yaml
```

The lock records the exact commit resolved for `v1.4.0`.

## 3. Ignore generated harness output

Add the harness directories to `.gitignore`. The exact paths depend on your selected harnesses; for Claude Code:

```bash
echo '.claude/skills/' >> .gitignore
```

## 4. Commit the configuration

```bash
git add .agents/config.yaml .agents/lock.yaml .gitignore
git commit -m "Add agent-marketplace configuration"
```

Teammates who clone the repository can reproduce the same output with `agent-marketplace sync`.

## Troubleshooting

- **`agent-marketplace sync` exits with a missing harness target**: Add `harnesses` to `.agents/config.yaml` or to `~/.agents/config.yaml`.
- **`ref` does not resolve**: Check that the tag or branch exists, or use `agent-marketplace lock --upgrade` to refresh to the current ref.
