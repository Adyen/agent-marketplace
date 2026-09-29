# Discover a skill with code mode in Devin

In this tutorial, you will connect Devin to the local `agent-marketplace` code-mode MCP server, find a skill in a practice registry, and read it without installing the skill. Everything you create for the lesson stays in a temporary repository.

## Before you begin

You need Git, Devin CLI, and an `agent-marketplace` binary that supports code mode. [Install the CLI](../install.md) if needed, then check that your binary lists the MCP command:

```bash
agent-marketplace mcp --help
```

Expect help for `mcp`, the stdio code-mode server. Keep the binary on `PATH` while you use this setup: the installed MCP entry launches `agent-marketplace mcp` by name.

## 1. Create a practice registry

Create an isolated Git repository with one harmless skill:

```bash
cd "$(mktemp -d /tmp/code-mode-tutorial.XXXXXX)"
git init --quiet
mkdir -p registry/skills/hello-code-mode .agents
cat > registry/skills/hello-code-mode/SKILL.md <<'EOF'
---
name: hello-code-mode
description: A greeting for the code-mode tutorial.
---

# Hello from code mode

Say: Hello from code mode.
EOF
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses: [devin]
sources:
  practice:
    directory: ./registry
EOF
cat .agents/config.yaml
```

Expect the v4 configuration shown above. It points to the local registry and selects Devin, but **does not select the skill for installation**. Keep this terminal in the practice repository for the rest of the lesson.

## 2. Register the code-mode server with Devin

Install the first-party `code-mode` collection from the Agent Marketplace Git registry, then synchronize the Devin MCP configuration:

```bash
agent-marketplace install code-mode --from https://github.com/Adyen/agent-marketplace --path skills
agent-marketplace sync
cat .devin/mcp_config.json
```

The collection selects the `agent-marketplace` MCP server and a bootstrap skill; `sync` should finish without errors. In the JSON file, find an `agent-marketplace` MCP server whose command launches `agent-marketplace mcp`. Notice that the practice `hello-code-mode` skill was not installed into `.devin/skills/`; only the code-mode collection was selected.

## 3. Find the skill from Devin

Start a new Devin CLI session **in the practice repository** so it reads the new `.devin/mcp_config.json`:

```bash
devin
```

Once the session opens, ask Devin to call the `agent-marketplace` MCP tool `search` with these arguments:

```json
{"code":"async () => catalog.filter(r => r.kind === 'skill' && r.name === 'hello-code-mode').map(r => ({name: r.name, ref: r.ref}))"}
```

The tool response contains a `value` with an entry like:

```json
[{"name":"hello-code-mode","ref":"local:registry/skill/hello-code-mode"}]
```

Notice the `ref`: it identifies the skill that the next call will load. Searching does not install it or start another server.

## 4. Load the skill on demand

Ask Devin to call the same MCP server's `execute` tool with the exact ref returned by `search`:

```json
{"servers":[],"code":"async () => (await skills.load('local:registry/skill/hello-code-mode')).content"}
```

The response's `value` contains the skill text, including `Say: Hello from code mode.` The empty `servers` list means this call does not start a registry-provided MCP server. The skill was loaded for this call, not installed as a permanent Devin skill.

## 5. Check what was recorded

Back in the practice-repository terminal, inspect the code-mode session log:

```bash
ls -t "${XDG_STATE_HOME:-$HOME/.local/state}/agent-marketplace/mcp-sessions/"*.jsonl
```

Expect at least one session-log file. The `skills.load` event records the skill ref, revision, and digest for audit and later promotion. Session logs are machine-local; they are not part of the practice repository.

## 6. Unregister the practice server

Leave the Devin session, then remove the `code-mode` collection and synchronize again:

```bash
agent-marketplace remove code-mode
agent-marketplace sync
```

Expect a sync without errors. The managed `agent-marketplace` entry is removed from this repo's `.devin/mcp_config.json`; your machine-local session log remains available for audit.

You have now discovered and loaded a skill dynamically without installing it. For the exact `search`, `execute`, and `promote` interfaces, see the [CLI reference](../reference/cli.md#mcp).
