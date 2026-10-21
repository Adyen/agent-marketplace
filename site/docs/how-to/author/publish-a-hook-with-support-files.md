# How to publish a hook with support files

Ship a hook whose command runs a script stored in your [registry](../../glossary.md#registry), so that every consumer gets both the hook entry and the script. The examples use the Claude Code layout and a `PreToolUse` hook that blocks `git push --force`.

## Prerequisites

- A Git registry in the Claude Code layout. For other layouts, see the [registry layout reference](../../reference/registry-layout.md).
- `agent-marketplace` on your `PATH`.
- Permission to push to the registry repository.

## The bundle root and the command form

`${CLAUDE_PLUGIN_ROOT}` stands for the bundle root: the folder that contains the folder holding the hook definition. For `hooks/hooks.json` that is the registry folder, so the path after the token starts with `hooks/`. For a workspace layout the path starts with that workspace folder: a Claude workspace hook in `.claude/settings.json` writes `${CLAUDE_PLUGIN_ROOT}/.claude/scripts/check.sh`, and Factory's `.factory/` and Cursor's `.cursor/` work the same way. Git and `directory` sources install the file and rewrite the command to reach it.

A Marketplace mirror always packages the files beside `hooks.json` as `hooks/<name>`, whatever folder holds the definition. So only a hook defined in `hooks/hooks.json` gets the same command on Git, `directory`, and mirror delivery. A hook that must be mirrored should use `hooks/hooks.json`. See [If consumers install from a Marketplace](#if-consumers-install-from-a-marketplace).

## 1. Put the hook and its files in one folder

In the Claude Code layout, the hook file is `hooks/hooks.json`. Put the files the hook needs in the same folder:

```text
hooks/
├── hooks.json
└── scripts/
    └── block-force-push.sh
```

Sync copies every regular file below `hooks/`, except `hooks.json` itself, into a support folder for each hook. Inside that support folder the file keeps its path relative to the bundle root: `hooks/scripts/block-force-push.sh` installs as `hooks/scripts/block-force-push.sh` below the hook's support folder. The bytes and permission bits are preserved. Keep only files the hook needs in this folder: a `README.md` or test fixture placed there reaches every consumer too.

If your registry uses the Claude Code workspace form instead, the hook file is `.claude/settings.json` and sync copies everything else below `.claude/`, including `.claude/skills/`. Prefer the `hooks/hooks.json` form when the registry also ships other resources.

## 2. Write the hook definition

Refer to the script with `${CLAUDE_PLUGIN_ROOT}`. This token stands for the bundle root, the folder that holds `hooks/`, so the path after it starts with `hooks/`:

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
            "command": "\"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/block-force-push.sh\"",
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

- Write `${CLAUDE_PLUGIN_ROOT}/hooks/scripts/...`, matching the file's path in the registry. Sync checks the reference against the files the hook ships: a path that names no shipped file or folder refuses the hook with `hook.support-reference-missing`. Commands written against the old folder-relative meaning, such as `${CLAUDE_PLUGIN_ROOT}/scripts/...`, still install for one minor release with a `hook.support-reference-legacy` warning; prefix the path with the folder named in the hint.
- Both `"${CLAUDE_PLUGIN_ROOT}/hooks/x.sh"` and `"${CLAUDE_PLUGIN_ROOT}"/hooks/x.sh` (only the token quoted) are checked.
- A literal reference to a file the hook does not ship refuses the hook with `hook.support-reference-missing`. That includes a log or state file the script writes at runtime, and `${CLAUDE_PLUGIN_ROOT}/..`. Write runtime files outside the support folder, for example under `$TMPDIR`.
- A bare `${CLAUDE_PLUGIN_ROOT}` or a path that contains a shell expansion or glob is not checked, so prefer a literal path and let sync catch a typo for you.
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
    + command: "\"$CLAUDE_PROJECT_DIR/.claude/hook-support/<key>/PreToolUse-command/hooks/scripts/block-force-push.sh\""

warning: materialize.hook.review: owned hook entries changed for this configuration; review the hook changes before use (settings.json)
  → Review the Hook changes listing
```

Every consumer sees this `Hook changes` listing when a hook command is added, changed, or removed, so they can review what will run. Check that the script landed at the path the command names:

```bash
find .claude/hook-support -type f
```

Expected output:

```text
.claude/hook-support/<key>/PreToolUse-command/hooks/scripts/block-force-push.sh
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

## If consumers install from a Marketplace

A Marketplace that mirrors your registry packages only the files that sit directly beside `hooks.json`. It skips subfolders. It stores each file as `hooks/<name>`, with its bytes and executable bit, and binds them to the hook's content revision. The command form is the same as everywhere else, `${CLAUDE_PLUGIN_ROOT}/hooks/<name>`, but `<name>` cannot contain a `/`, so a hook that must also be Marketplace-mirrored keeps its scripts flat in `hooks/`. Move the script from step 1 up one level, out of `scripts/`:

```bash
git mv hooks/scripts/block-force-push.sh hooks/block-force-push.sh
```

and change the `command` line in `hooks/hooks.json` to:

```json
"command": "\"${CLAUDE_PLUGIN_ROOT}/hooks/block-force-push.sh\"",
```

Follow these rules for the command:

- Name each script as `${CLAUDE_PLUGIN_ROOT}/hooks/<file>`, where `<file>` has no `/`. A reference into a subfolder, such as `${CLAUDE_PLUGIN_ROOT}/hooks/lib/...`, falls outside what the Marketplace can package.
- `${DROID_PLUGIN_ROOT}`, `${CURSOR_PLUGIN_ROOT}`, `${PLUGIN_ROOT}`, and `${extensionPath}` follow the same rule, with or without braces.
- Every other file directly in `hooks/` is packaged too, so keep notes and fixtures elsewhere.

The scratch project from step 5 works for this flat layout too: a `directory` source and a mirror of the same revision now install the file and the command identically. To check the Marketplace path end to end, publish the registry to a local Marketplace, as in [Tutorial: run a local Marketplace](../../tutorials/run-a-local-marketplace.md), and install the hook from there.

When the Marketplace operator next refreshes your registry, the changed hook gets a new content revision. Consumers already pinned to the old revision keep it until they install the new one.

## Upgrade note: support folder layout

This release installs support files at `<key>/<D>/<rel>`, where `<D>` is the folder that holds the hook definition (`hooks`, `.claude`, and so on). Earlier releases installed them at `<key>/<rel>`. A client older than this release moves them back to the old layout on its next sync, so `sync --check` reports drift on whichever client did not sync last, including in CI. Upgrade every client that syncs the same repository, CI's pinned client included, together. See [How to troubleshoot sync drift](../troubleshoot-sync-drift.md) for the symptom.

## Troubleshooting

- **The hook runs but reports `No such file or directory`:** the command path does not match a file below the hook's support folder. Open the `Hook changes` listing or the target's hook file, and check that the path after `PreToolUse-command/` exists under `hook-support/`. A literal `${CLAUDE_PLUGIN_ROOT}/...` reference is checked at sync, so this happens only with a bare token, a shell expansion, or a glob in the path.
- **Sync refuses the hook with `hook.support-reference-missing`:** the path after `${CLAUDE_PLUGIN_ROOT}` names no file the hook ships, read from the bundle root. The hook is skipped and sibling hooks still install. Fix the path or add the file. A missing folder prefix is the usual cause: write the bundle-root path, such as `${CLAUDE_PLUGIN_ROOT}/hooks/scripts/...` for `hooks/hooks.json`, not `${CLAUDE_PLUGIN_ROOT}/scripts/...`.
- **Sync warns `hook.support-reference-legacy`:** the command uses the old folder-relative form, such as `${CLAUDE_PLUGIN_ROOT}/scripts/...`, which still installs for one minor release. Prefix the path after the token with the folder named in the warning's hint, such as `hooks/`, before the fallback is removed.
- **The hook reports `Permission denied`:** the script was committed without the executable bit. Run `chmod +x` on it, commit, and release again.
- **Sync reports `hook.env-unrepresentable`:** the command puts the token inside single quotes. Use double quotes. For some targets, such as Windsurf, sync also refuses a project-folder variable joined to other text, as in `${CLAUDE_PROJECT_DIR}-backup`; put a `/` after the variable.
- **The hook installs for one harness but not another:** that harness has no exact match for the event, matcher, handler type, or timeout. Sync names the cause in an error such as `hook.event-unmappable` and still installs the hook for the other targets. Check the [hook and tool names reference](../../reference/hook-and-tool-names.md).
- **The Marketplace reports `marketplace.ingest.hook-support-unavailable` when it publishes the registry:** the command names a script the mirror cannot package, or a file beside `hooks.json` is unsafe to read, such as a symlink. Use the Marketplace form above, and replace symlinks with regular files.
- **A Marketplace consumer gets `marketplace.hook.support-unsupported`:** the command names a support file the mirror did not package, such as one in a subfolder of `hooks/`. Either move the file beside `hooks.json` and release the hook, or have the consumer add your registry as a Git source.
- **A Marketplace consumer gets `marketplace.hook.support-missing`:** the command names a `hooks/<file>` that the published revision does not contain. Add the file, release, and ask the operator to refresh the registry. Consumers then have to install the new revision.
- **`list --from .` shows a `malformed:` status:** the status names the diagnostic, such as `json.duplicate-key` for a key that appears twice. See the [diagnostics reference](../../reference/diagnostics.md).

## Related documentation

- [Registry layout reference](../../reference/registry-layout.md)
- [Hook and tool names reference](../../reference/hook-and-tool-names.md)
- [Harnesses and output paths reference](../../reference/harnesses-and-output-paths.md)
- [How to validate a registry in CI](./validate-a-registry-in-ci.md)
