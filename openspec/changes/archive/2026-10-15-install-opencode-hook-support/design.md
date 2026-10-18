## Decisions

### Support is declared by the layout

A partition whose hooks are resource files declares `HookSupportDir`. It must be a component and must not coexist with a hook aggregate, whose `SupportDir` already owns the role. OpenCode lists `hook-support` before `hooks`, so support publishes before the declarations that reference it.

### Capture is shared; publication is ordinary

Support capture reuses the aggregate path: a safe source snapshot, refusal of links and oversize input, cross-hook conflict claims, and the source-changed check. Publication uses the ordinary manifest-owned artifact pipeline that already installs OpenCode declarations, so prune, collision, and blocked-component rules apply unchanged. Sibling OpenCode declarations in a source directory are other hooks and are excluded.

### The adapter supplies the project directory

Handlers run from the OpenCode instance directory, which can be a subdirectory of the repository. The adapter therefore sets `AGENT_MARKETPLACE_PROJECT_DIR` to the directory that contains its own `.opencode` root, or to the instance directory for an adapter in any other configuration directory. The hook catalog declares it as the repository base. A repository selection relocated outside the repository by `OPENCODE_CONFIG_DIR` uses absolute support paths, which only exist on that machine anyway.

### Review compares owned declarations

The review reads owned declarations before publication: manifest-recorded files, or links into the target's compiled cache. It compares them with the declarations after publication, or with the desired set when checking. While the hook component is blocked, retained declarations stay in the desired set. Linked declarations keep their link target when their content changes, so pending changes are also recorded as pending outputs for `sync --check`.

## Risks

- `sync --check` already misses content changes in symlinked resources whose link target is stable. This change covers hook declarations only.
- Changing adapter bytes requires an adapter refresh in source layouts; the adapter has not been released yet.
