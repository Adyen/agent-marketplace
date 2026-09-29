## ADDED Requirements

### Requirement: Opt-in registration

Code mode SHALL be off by default. A config layer opts in by declaring
`codeMode.enabled: true`; only v4 configuration roots accept the `codeMode`
block, and only `enabled` is a supported field. While the merged effective
value is true, `sync` SHALL add a required owned `agent-marketplace` MCP
entry running `agent-marketplace mcp` to every target harness; when the
effective value is false or absent, `sync` SHALL NOT register it and SHALL
remove any previously owned entry while preserving handwritten entries.
Layer merge follows ordinary precedence. `agent-marketplace mcp enable`
writes `enabled: true` and `mcp disable` removes the block in one selected
layer (`--scope user|repo|local`); `init --code-mode` enables it in the
initialized config.

#### Scenario: Sync registers owned server entry while enabled

- **WHEN** `codeMode.enabled` is effectively true and sync runs
- **THEN** every target harness gains an owned `agent-marketplace` MCP entry
  launching `agent-marketplace mcp`, and disabling plus re-syncing removes
  only the owned entry

#### Scenario: Disabled or absent codeMode registers nothing

- **WHEN** no layer enables `codeMode.enabled`
- **THEN** sync registers no code-mode MCP entry

#### Scenario: Layer precedence resolves effective enable

- **WHEN** layers disagree on `codeMode.enabled`
- **THEN** the higher-precedence layer's value wins, and no layer present
  means disabled

#### Scenario: Enable and disable write one config layer

- **WHEN** `mcp enable --scope <s>` or `mcp disable --scope <s>` runs
- **THEN** only that layer's `codeMode.enabled` is set or removed, and the
  output directs the user to run `sync`

#### Scenario: Writes refuse legacy configuration roots

- **WHEN** `mcp enable` targets a non-v4 root
- **THEN** it refuses with a `v4.writer-opt-in` error naming the migrate
  command and leaves the file unchanged

#### Scenario: Init flag opts in

- **WHEN** `agent-marketplace init --code-mode` runs
- **THEN** the written config contains `codeMode.enabled: true`

### Requirement: Two-tool MCP surface over stdio

`agent-marketplace mcp` SHALL serve JSON-RPC 2.0 over stdio at protocol
version `2025-06-18`, identifying as `agent-marketplace`/`code-mode`, and
`tools/list` SHALL return exactly the tools `search` and `execute`.
Notifications receive no response; unknown request methods fail with
`-32601`; malformed lines fail with `-32700`. Tool-level failures SHALL be
returned as `isError` tool results carrying a stable coded error, never as
process exits; the process ends on input EOF or shutdown signal only.

#### Scenario: tools/list exposes exactly search and execute

- **WHEN** a client initializes and lists tools
- **THEN** the result contains `search` and `execute` and nothing else

#### Scenario: Requests answered over stdio

- **WHEN** a client sends initialize, tools/list, and tools/call requests
- **THEN** each request receives a matching JSON-RPC response and tool
  failures arrive as coded `isError` results

#### Scenario: Server exits on shutdown

- **WHEN** the stdio input closes or the process is signaled
- **THEN** the server exits without requiring client action, cancelling
  pending tool calls

### Requirement: Read-only search over a catalog snapshot

`search({code})` SHALL run the submitted async arrow function with only the
`catalog` binding: a read-only snapshot merged from every configured
registry and Marketplace. Search SHALL never start a server, load skill
content, or write. When the snapshot is stale, results SHALL carry the
`catalog.stale` warning.

#### Scenario: Search binds only the catalog

- **WHEN** a search function reads `catalog`
- **THEN** it sees the snapshot's resource entries and no `skills` or `mcp`
  binding exists

#### Scenario: Search never starts servers

- **WHEN** a search function executes
- **THEN** no MCP server process is started

#### Scenario: Stale snapshot warns

- **WHEN** the catalog snapshot is marked stale
- **THEN** the search result includes the `catalog.stale` warning

### Requirement: Declared-server execution

`execute({servers, code})` SHALL accept only exact catalog refs of kind
`mcp` in `servers`; malformed or unmatched refs fail `resource.not-found`,
non-`mcp` kinds fail `resource.unsupported-kind`, and gate checks for every
declared ref complete before the function runs. Declared servers bind under
`mcp.<alias>` where the alias is the resource name, disambiguated by
registry when names collide; accessing an undeclared alias fails
`mcp.not-declared` and an alias that cannot be made unique fails
`codemode.invalid-arguments`. `skills.load(ref)`/`skills.read(ref, path)`
accept only catalog refs of kind `skill` and pass the same gate.

#### Scenario: Declared refs must be catalog MCP servers

- **WHEN** `servers` names a ref of a kind other than `mcp`
- **THEN** execution fails with `resource.unsupported-kind`

#### Scenario: Unknown or malformed refs rejected

- **WHEN** `servers` names a ref absent from the catalog or malformed
- **THEN** execution fails with `resource.not-found`

#### Scenario: Undeclared alias rejected

- **WHEN** code reads `mcp.<name>` for a server not listed in `servers`
- **THEN** the call fails with `mcp.not-declared`

#### Scenario: Colliding aliases disambiguated by registry

- **WHEN** two declared servers share a resource name
- **THEN** they bind as `<registry>-<name>` aliases

#### Scenario: Ambiguous alias fails closed

- **WHEN** two declared refs resolve to the same alias
- **THEN** execution fails with `codemode.invalid-arguments` and no server
  starts

#### Scenario: Declared servers gated before code runs

- **WHEN** one declared ref is revoked or rejected by the gate
- **THEN** execution fails with the gate's coded error and no code or
  server runs

#### Scenario: Skills load and read on demand

- **WHEN** code calls `skills.load` or `skills.read` on a catalog skill
- **THEN** the content, files, findings, and digest are returned and the
  access is session-logged; non-skill refs fail
  `resource.unsupported-kind`

### Requirement: Sandboxed script execution

Each call SHALL run the submitted function in a fresh JavaScript VM with no
host globals beyond the declared bindings, under a 30 s deadline, a 1 MiB
serialized-output cap, and a 100 host-call cap. Breaches SHALL fail with
`codemode.timeout`, `codemode.output-limit`, or
`codemode.host-call-limit`; oversized output is truncated to a valid JSON
marker flagged `truncated`. Script and host failures surface as stable
coded errors catchable by script code, and TypeScript annotations are
stripped before evaluation.

#### Scenario: Deadline bounds execution

- **WHEN** a function or its host call exceeds the deadline
- **THEN** the call fails with `codemode.timeout`

#### Scenario: Output cap truncates to valid JSON

- **WHEN** the serialized result exceeds the output cap
- **THEN** the result is a valid JSON truncation marker with `truncated`
  set

#### Scenario: Host-call cap enforced

- **WHEN** a function exceeds the host-call limit
- **THEN** the call fails with `codemode.host-call-limit`

#### Scenario: No ambient host globals

- **WHEN** a function probes for filesystem, network, or process globals
- **THEN** none are present

#### Scenario: Failures carry stable codes

- **WHEN** a function throws or a binding fails
- **THEN** the tool result carries the coded error and script `try/catch`
  can observe the same code

### Requirement: Skill content access safety

Skill file reads SHALL be confined to the resource's content root:
`..` segments and symlink components are rejected, only regular files up to
4 MiB are served, and support-file lists are bounded. Marketplace resources
SHALL be gated on verified content: the cached or fetched artifact is
validated against its archive and content digests, revoked revisions fail
`resource.revoked`, and rejected assessments fail `resource.rejected`
before any bytes reach the script.

#### Scenario: Traversal and symlink escape rejected

- **WHEN** `skills.read` is given a path with `..` segments or a symlinked
  component
- **THEN** the read fails `resource.not-found` and no external bytes are
  returned

#### Scenario: Marketplace artifacts verified by digest

- **WHEN** a Marketplace skill or server resolves its artifact
- **THEN** archive and content digests are verified before content or the
  server definition is used, and failures are coded errors

#### Scenario: Revoked or rejected resources refuse

- **WHEN** the gate or assessment marks a resource revoked or rejected
- **THEN** load, read, and execute fail with `resource.revoked` or
  `resource.rejected`

### Requirement: MCP server lifecycle security

Declared servers SHALL start lazily on first `listTools` or tool call, one
process per exact ref and definition digest, and only environment variables
declared in the server definition are forwarded. A crashed or timed-out
server SHALL NOT be restarted within the session, and closing the session
stops every child. Remote `url` servers SHALL require `https` or an exact
loopback host, never follow redirects, and send only declared headers;
start failures report bounded, secret-redacted stderr.

#### Scenario: Crashed server is not restarted

- **WHEN** a started server exits or times out
- **THEN** subsequent calls fail `mcp.tool-error` without a respawn

#### Scenario: Only declared environment forwarded

- **WHEN** a stdio server starts
- **THEN** its environment contains only the definition's declared `env`
  entries

#### Scenario: Remote endpoints require HTTPS or loopback

- **WHEN** a server definition's `url` is plain `http` to a non-loopback
  host
- **THEN** startup fails `mcp.start-failed`

#### Scenario: Hung server killed at deadline

- **WHEN** a server fails to answer within the pool deadline
- **THEN** the call fails `codemode.timeout` and the process is killed

#### Scenario: Session close stops child servers

- **WHEN** the engine pool closes
- **THEN** every spawned child process is terminated

### Requirement: Session-log identity evidence

Every `skills.load`, `skills.read`, and lazily started server SHALL append
a JSONL entry under
`$XDG_STATE_HOME/agent-marketplace/mcp-sessions/` recording the event,
exact catalog ref, revision, content or archive digest, and the Git source
URL/path or `marketplace:` provenance needed to reinstall it.

#### Scenario: Loads and starts recorded with identity

- **WHEN** a skill loads or a declared server starts
- **THEN** a session entry records the event, ref, revision, and digest

#### Scenario: Source provenance recorded

- **WHEN** the resource came from a Git or directory source
- **THEN** the entry carries the source URL and path; Marketplace resources
  carry `marketplace:` provenance

### Requirement: Promotion to durable selection

`agent-marketplace promote <ref>` SHALL locate the most recent
`skill.load` or `server.start` session entry matching the exact ref and
route it through the normal install transaction using the recorded
provenance: Git sources reinstall at the recorded source and locked
commit, and Marketplace provenance installs a locator pinned to the
recorded digest. Marketplace findings and active capabilities apply the
same consent gate as `install`; `--accept-risk`/`--yes` are rejected for
non-Marketplace resources. With no matching entry, or an entry whose
recorded provenance cannot route safely, promote SHALL fail closed.

#### Scenario: Missing evidence fails

- **WHEN** no session entry matches the ref
- **THEN** promote fails reporting nothing was loaded through code mode

#### Scenario: Latest matching entry wins

- **WHEN** multiple sessions recorded the ref
- **THEN** the most recent `skill.load` or `server.start` entry is used

#### Scenario: Recorded Git source installed at recorded commit

- **WHEN** the entry records a Git source URL, path, and revision
- **THEN** install selects that source and locks the recorded commit

#### Scenario: Marketplace promotion pins exact digest

- **WHEN** the entry has Marketplace provenance
- **THEN** install resolves the resource locator with the recorded digest

#### Scenario: Marketplace findings gate on consent

- **WHEN** the promoted Marketplace resource carries findings or active
  capabilities
- **THEN** interactive runs prompt and non-interactive runs refuse until
  `--accept-risk` or `--yes` is passed

#### Scenario: Risk flags rejected for non-Marketplace sources

- **WHEN** `--accept-risk` or `--yes` accompanies a Git or directory entry
- **THEN** promote fails naming the restriction

#### Scenario: Unprovenanced ref fails closed

- **WHEN** an entry records a colon-bearing ref with no usable provenance
- **THEN** promote refuses and directs the user to reload through code mode
