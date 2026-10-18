# How to publish a hook with support files

Ship a hook whose command runs a script stored in your [registry](../../glossary.md#registry), so that every consumer gets both the hook entry and the script. The examples use the Claude Code layout and a `PreToolUse` hook that blocks `git push --force`.

## Prerequisites

- A Git registry in the Claude Code layout. For other layouts, see the [registry layout reference](../../reference/registry-layout.md).
- `agent-marketplace` on your `PATH`.
- Permission to push to the registry repository.

## 1. Put the hook and its files in one folder

In the Claude Code layout, the hook file is `hooks/hooks.json`. Put the files the hook needs in the same folder:

```text
hooks/
├── hooks.json
└── scripts/
    └── block-force-push.sh
```

Sync copies every regular file below `hooks/`, except `hooks.json` itself, into a support folder for each hook. It keeps the relative paths, the bytes, and the permission bits. Keep only files the hook needs in this folder: a `README.md` or test fixture placed there reaches every consumer too.

If your registry uses the Claude Code workspace form instead, the hook file is `.claude/settings.json` and sync copies everything else below `.claude/`, including `.claude/skills/`. Prefer the `hooks/hooks.json` form when the registry also ships other resources.

## 2. Write the hook definition

Refer to the script with `${CLAUDE_PLUGIN_ROOT}`. In a registry, this token stands for the folder that holds the hook file, here `hooks/`, not the registry root:

```bash
mkdir -p hooks/scripts
cat > hooks/hooks.json << 'EOF'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}/scripts/block-force-push.sh\"",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
EOF
```

Both commands print nothing. Follow these rules for the command:

- Write `${CLAUDE_PLUGIN_ROOT}/scripts/...`, not `${CLAUDE_PLUGIN_ROOT}/hooks/scripts/...`. Sync does not check that the path exists, so the wrong form installs a command that points at a missing file.
- Wrap the path in double quotes. Sync refuses a token inside single quotes with `hook.env-unrepresentable`.
- Use a matcher made of exact tool names, such as `Bash` or `Edit|Write`, if consumers target other harnesses. Sync translates exact names and refuses patterns it cannot map. The [hook and tool names reference](../../reference/hook-and-tool-names.md) lists the names and events each harness accepts.

Sync replaces the token with the path of that hook's support folder in each consumer's checkout. Consumers do not need to set any variable.

## 3. Write the script and make it executable

```bash
cat > hooks/scripts/block-force-push.sh << 'EOF'
#!/bin/sh
# Refuse `git push --force` before Claude Code runs it.
if grep -q 'git push.*--force'; then
  echo "Force-push is blocked by registry policy." >&2
  exit 2
fi
EOF
chmod +x hooks/scripts/block-force-push.sh
```

Sync copies the permission bits, so the script must be executable when you commit it. Git records the executable bit.

The harness passes the tool call to the script as JSON on standard input, and reads its exit code. Write the script for the hook contract of each harness your consumers use. Test it by hand:

```bash
echo '{"tool_input":{"command":"git push --force origin main"}}' | hooks/scripts/block-force-push.sh; echo "exit=$?"
echo '{"tool_input":{"command":"git status"}}' | hooks/scripts/block-force-push.sh; echo "exit=$?"
```

Expected output:

```text
Force-push is blocked by registry policy.
exit=2
exit=0
```

## 4. Check the hook name

From the registry root, list what `agent-marketplace` discovers:

```bash
agent-marketplace list --from .
```

Expected output:

```text
REGISTRY  KIND  NAME                TAGS (metadata)  STATUS
.         hook  PreToolUse-command                   available
```

Each handler becomes one hook named `<event>-<type>`. A second `PreToolUse` command handler in the same file becomes `PreToolUse-command-2`, and so on in file order. Consumers select hooks by these names, so adding a handler before an existing one of the same event and type renames it. Add new handlers at the end.

## 5. Try the hook in a scratch project

Create an empty Git repository and point a `directory` source at your registry checkout:

```bash
mkdir -p /tmp/hook-try/.agents && cd /tmp/hook-try && git init -q
cat > .agents/config.yaml << EOF
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:
  draft:
    directory: $OLDPWD
    source_harness: claude
resources:
  draft:
    include:
      - hook/PreToolUse-command
EOF
agent-marketplace sync
```

`$OLDPWD` is the registry folder you left with `cd`. A directory source reads the folder as it is, so you do not need to commit first. Typical output (`<key>` stands for a source key such as `my-registry-911adb…`):

```text
Installed 2  Removed 0  Malformed 0  Errors 0
  → /tmp/hook-try/.claude (1 written), /tmp/hook-try/.claude/hook-support (1 written)
Hook changes:
  claude (repository) add /tmp/hook-try/.claude/settings.json: PreToolUse matcher "Bash"
    + command: "\"$CLAUDE_PROJECT_DIR/.claude/hook-support/<key>/PreToolUse-command/scripts/block-force-push.sh\""

warning: materialize.hook.review: owned hook entries changed for this configuration; review the hook changes before use (settings.json)
  → Review the Hook changes listing
```

Every consumer sees this `Hook changes` listing when a hook command is added, changed, or removed, so they can review what will run. Check that the script landed at the path the command names:

```bash
find .claude/hook-support -type f
```

Expected output:

```text
.claude/hook-support/<key>/PreToolUse-command/scripts/block-force-push.sh
```

The source key depends on the source and its revision, so it changes between releases. Do not write it into your hook commands or docs; use `${CLAUDE_PLUGIN_ROOT}`.

To see the result for other harnesses, add them to `harnesses`, for example `[claude, codex]`, and sync again. Each target gets its own copy of the script under its own root, and a command that starts from that harness's repository folder. For Codex, the path starts with `$(git rev-parse --show-toplevel)/.codex/hook-support/` and the matcher becomes `^Bash$`. At user scope, commands use absolute paths.

## 6. Commit, tag, and push

```bash
git add hooks/hooks.json hooks/scripts/block-force-push.sh
git commit -m "Add force-push guard hook"
git tag v1.6.0
git push origin main v1.6.0
```

Tell consumers to select the hook by name:

```yaml
resources:
  platform:
    include:
      - hook/PreToolUse-command
```

Consumers on `ref: v1.6.0` get it on their next `agent-marketplace sync`. For branch refs and older tags, see [How to version and release a registry](./version-and-release-a-registry.md).

## Troubleshooting

- **The hook runs but reports `No such file or directory`:** the command path does not match a file below the hook folder. Open the `Hook changes` listing or the target's hook file, and check that the path after `PreToolUse-command/` exists under `hook-support/`. The usual cause is an extra `hooks/` after `${CLAUDE_PLUGIN_ROOT}`.
- **The hook reports `Permission denied`:** the script was committed without the executable bit. Run `chmod +x` on it, commit, and release again.
- **Sync reports `hook.env-unrepresentable`:** the command puts the token inside single quotes. Use double quotes. For some targets, such as Windsurf, sync also refuses a project-folder variable joined to other text, as in `${CLAUDE_PROJECT_DIR}-backup`; put a `/` after the variable.
- **The hook installs for one harness but not another:** that harness has no exact match for the event, matcher, handler type, or timeout. Sync names the cause in an error such as `hook.event-unmappable` and still installs the hook for the other targets. Check the [hook and tool names reference](../../reference/hook-and-tool-names.md).
- **`list --from .` shows a `malformed:` status:** the status names the diagnostic, such as `json.duplicate-key` for a key that appears twice. See the [diagnostics reference](../../reference/diagnostics.md).

## Related documentation

- [Registry layout reference](../../reference/registry-layout.md)
- [Hook and tool names reference](../../reference/hook-and-tool-names.md)
- [Harnesses and output paths reference](../../reference/harnesses-and-output-paths.md)
- [How to validate a registry in CI](./validate-a-registry-in-ci.md)
