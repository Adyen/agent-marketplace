# Factory Droid validation

## Native installation contract

Factory MCP configuration is materialized in the target's native aggregate:

- Repository scope: `<repo>/.factory/mcp.json`
- User scope: `~/.factory/mcp.json`

The materializer preserves handwritten servers and refuses unmanaged name collisions. Skills, commands, droids, hooks, and rules have separate generated-layout assertions.

## Native probe

The CI image contains the pinned `droid` CLI. The probe runs:

```text
droid --version
droid mcp list
```

A negative control requires the two fixture names to be absent before synchronization. After collection installation and `sync`, both names must appear in `droid mcp list`. The test records configuration recognition only; it does not claim a server connection or tool execution because the probe is intentionally offline.

Factory MCP listing may render status text differently between CLI versions. Assertions therefore use stable fixture identities and explicit failure words rather than full-output goldens.

## Configuration-only coverage

Skills, converted commands, custom droids, hooks, and rules are verified through the generated filesystem and compiler contracts. Factory command files use the file materialization workaround required by the current CLI; the test must continue to cover both configured symlink/file policy and the target-specific command representation.

## Adding Factory coverage

Use a short, stable fixture name when comparing materialization modes. Keep a separate long-name case if a future CLI appears to truncate or wrap list output. Do not treat a partially rendered name as discovery success.
