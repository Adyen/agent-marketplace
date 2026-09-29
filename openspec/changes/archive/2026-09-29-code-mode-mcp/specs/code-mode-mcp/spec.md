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
#### Scenario: Disabled or absent codeMode registers nothing
#### Scenario: Layer precedence resolves effective enable
#### Scenario: Enable and disable write one config layer
#### Scenario: Writes refuse legacy configuration roots
#### Scenario: Init flag opts in
### Requirement: Two-tool MCP surface over stdio

`agent-marketplace mcp` SHALL serve JSON-RPC 2.0 over stdio at protocol
version `2025-06-18`, identifying as `agent-marketplace`/`code-mode`, and
`tools/list` SHALL return exactly the tools `search` and `execute`.
Notifications receive no response; unknown request methods fail with
`-32601`; malformed lines fail with `-32700`. Tool-level failures SHALL be
returned as `isError` tool results carrying a stable coded error, never as
process exits; the process ends on input EOF or shutdown signal only.

#### Scenario: tools/list exposes exactly search and execute
#### Scenario: Requests answered over stdio
#### Scenario: Server exits on shutdown
### Requirement: Read-only search over a catalog snapshot

`search({code})` SHALL run the submitted async arrow function with only the
`catalog` binding: a read-only snapshot merged from every configured
registry and Marketplace. Search SHALL never start a server, load skill
content, or write. When the snapshot is stale, results SHALL carry the
`catalog.stale` warning.

#### Scenario: Search binds only the catalog
#### Scenario: Search never starts servers
#### Scenario: Stale snapshot warns
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
#### Scenario: Unknown or malformed refs rejected
#### Scenario: Undeclared alias rejected
#### Scenario: Colliding aliases disambiguated by registry
#### Scenario: Ambiguous alias fails closed
#### Scenario: Declared servers gated before code runs
#### Scenario: Skills load and read on demand
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
#### Scenario: Output cap truncates to valid JSON
#### Scenario: Host-call cap enforced
#### Scenario: No ambient host globals
#### Scenario: Failures carry stable codes
### Requirement: Skill content access safety

Skill file reads SHALL be confined to the resource's content root:
`..` segments and symlink components are rejected, only regular files up to
4 MiB are served, and support-file lists are bounded. Marketplace resources
SHALL be gated on verified content: the cached or fetched artifact is
validated against its archive and content digests, revoked revisions fail
`resource.revoked`, and rejected assessments fail `resource.rejected`
before any bytes reach the script.

#### Scenario: Traversal and symlink escape rejected
#### Scenario: Marketplace artifacts verified by digest
#### Scenario: Revoked or rejected resources refuse
### Requirement: MCP server lifecycle security

Declared servers SHALL start lazily on first `listTools` or tool call, one
process per exact ref and definition digest, and only environment variables
declared in the server definition are forwarded. A crashed or timed-out
server SHALL NOT be restarted within the session, and closing the session
stops every child. Remote `url` servers SHALL require `https` or an exact
loopback host, never follow redirects, and send only declared headers;
start failures report bounded, secret-redacted stderr.

#### Scenario: Crashed server is not restarted
#### Scenario: Only declared environment forwarded
#### Scenario: Remote endpoints require HTTPS or loopback
#### Scenario: Hung server killed at deadline
#### Scenario: Session close stops child servers
### Requirement: Session-log identity evidence

Every `skills.load`, `skills.read`, and lazily started server SHALL append
a JSONL entry under
`$XDG_STATE_HOME/agent-marketplace/mcp-sessions/` recording the event,
exact catalog ref, revision, content or archive digest, and the Git source
URL/path or `marketplace:` provenance needed to reinstall it.

#### Scenario: Loads and starts recorded with identity
#### Scenario: Source provenance recorded
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
#### Scenario: Latest matching entry wins
#### Scenario: Recorded Git source installed at recorded commit
#### Scenario: Marketplace promotion pins exact digest
#### Scenario: Marketplace findings gate on consent
#### Scenario: Risk flags rejected for non-Marketplace sources
#### Scenario: Unprovenanced ref fails closed