# code-mode-mcp Specification

## Purpose
Defines the opt-in code-mode MCP surface: how a config layer enables
registration of the `agent-marketplace mcp` server, what that server exposes
over stdio, how `search` and `execute` behave inside a bounded JavaScript
sandbox, how dynamically used resources are identified, and how `promote`
turns session evidence into a durable installed selection.

## Requirements

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
declared ref complete before the function runs. A ref listed more than once
in `servers` SHALL be treated as a single declaration. Declared servers bind
under `mcp.<alias>` where the alias is the resource name, disambiguated by
registry when distinct declared refs share a name; accessing an undeclared
alias fails `mcp.not-declared` and an alias that cannot be made unique fails
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

- **WHEN** two distinct declared servers share a resource name
- **THEN** they bind as `<registry>-<name>` aliases

#### Scenario: Repeated declared ref binds once

- **WHEN** `servers` lists the same ref more than once
- **THEN** it binds once under its resource name alias, `Object.keys(mcp)`
  lists that alias only once, and at most one server process starts

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
`codemode.host-call-limit`. A result whose serialized form exceeds the
output cap SHALL be replaced by the valid JSON marker
`{"truncated": true, "bytes": <serialized size>, "prefix": <string>}`,
whose `prefix` holds at most the first 4 KiB of the serialized value, and
the tool result SHALL be flagged `truncated`. Script and host failures
surface as stable coded errors. An error thrown into script code SHALL be a
JavaScript `Error` whose `name` is `CodeModeError` and whose `code` and
`message` equal the tool-level error's `code` and `message`; it SHALL NOT
expose Go-cased fields. TypeScript annotations are stripped before
evaluation.

#### Scenario: Deadline bounds execution

- **WHEN** a function or its host call exceeds the deadline
- **THEN** the call fails with `codemode.timeout`

#### Scenario: Output cap truncates to valid JSON

- **WHEN** the serialized result exceeds the output cap
- **THEN** the result is a valid JSON truncation marker with `truncated`
  set, `bytes` equal to the serialized size, and a `prefix` of at most
  4 KiB, and the tool result is flagged `truncated`

#### Scenario: Host-call cap enforced

- **WHEN** a function exceeds the host-call limit
- **THEN** the call fails with `codemode.host-call-limit`

#### Scenario: No ambient host globals

- **WHEN** a function probes for filesystem, network, or process globals
- **THEN** none are present

#### Scenario: Failures carry stable codes

- **WHEN** a function throws or a binding fails
- **THEN** the tool result carries the coded error and script `try/catch`
  observes an `Error` instance named `CodeModeError` with the same
  lowercase `code` and `message`, and no `Code` or `Message` field

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
declared in the server definition are forwarded. Before a declared server
starts, complete `${VAR}` and `${env:VAR}` references in its `command`,
`args`, `env` values, `url`, and `headers` SHALL be expanded from the host
environment. A reference to an unset variable, a `${file:...}` reference,
or any other well-formed `${...}` reference SHALL fail `mcp.start-failed`
naming the field and variable but no value, and no unexpanded reference
SHALL reach the server. Expansion SHALL NOT add variables to the child
environment. A crashed or timed-out server SHALL NOT be restarted within
the session, and closing the session stops every child. Remote `url`
servers SHALL require `https` or an exact loopback host, never follow
redirects, and send only declared headers. A server that rejects or fails
its initialization handshake SHALL fail `mcp.start-failed`. Start failures
report bounded stderr or response bodies with host-environment values that
were forwarded or expanded redacted.

#### Scenario: Crashed server is not restarted

- **WHEN** a started server exits or times out
- **THEN** subsequent calls fail `mcp.tool-error` without a respawn

#### Scenario: Only declared environment forwarded

- **WHEN** a stdio server starts
- **THEN** its environment contains only the definition's declared `env`
  entries

#### Scenario: Environment references expanded before start

- **WHEN** a declared server definition uses `${VAR}` or `${env:VAR}` in
  its args, env values, url, or headers and those variables are set on the
  host
- **THEN** the started process or remote endpoint receives the expanded
  values, and the child environment still contains only the declared `env`
  keys

#### Scenario: Unresolvable reference fails before start

- **WHEN** a declared server definition references an unset variable or
  uses a `${file:...}` reference
- **THEN** the call fails `mcp.start-failed` naming the field and
  variable, no process starts, and no request is sent

#### Scenario: Remote initialization rejection reports start failure

- **WHEN** a remote server answers the initialization handshake with an
  HTTP error status
- **THEN** the call fails `mcp.start-failed` with a bounded response body
  in which expanded header values are redacted

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

### Requirement: Collection-based registration

Code mode SHALL be off by default. The first-party Git registry SHALL publish a `code-mode` collection containing one bootstrap skill and one `agent-marketplace` MCP server resource launching the installed `agent-marketplace mcp` executable. Ordinary resource installation SHALL select both resources, and sync SHALL materialize them only for supported selected harness targets. An unavailable executable or unrepresentable resource SHALL produce a visible actionable failure rather than reporting usable code mode. Ordinary collection removal SHALL remove only owned entries no longer selected elsewhere, preserve handwritten MCP entries and unrelated settings, and refuse unowned collisions. Repeated install, sync, and remove SHALL be idempotent. The former `codeMode` configuration field and the `mcp enable`, `mcp disable`, and `init --code-mode` CLI options SHALL NOT be accepted. Loading a root or fragment with `codeMode` SHALL fail without rewriting that file, with a diagnostic directing the user to remove the field and install the collection. The `agent-marketplace mcp` stdio runtime SHALL remain available with the same `search` and `execute` tool contract.

#### Scenario: Installing collection selects skill and MCP

- **WHEN** a user installs the first-party `code-mode` collection from its Git registry and syncs a supported target
- **THEN** its bootstrap skill and owned `agent-marketplace mcp` server materialize; a missing executable or unsupported target reports an actionable failure, and an unselected fresh config installs neither

#### Scenario: Removing collection preserves other owners

- **WHEN** a user removes the `code-mode` collection and syncs, including a case where one member is still independently selected or the MCP aggregate contains handwritten entries
- **THEN** only unselected owned entries are removed, handwritten entries survive, unowned collisions are refused, and repeating the operations does not change the resulting state

#### Scenario: Retired enablement fails with migration guidance

- **WHEN** a root or included config contains `codeMode`, or a user invokes `mcp enable`, `mcp disable`, or `init --code-mode`
- **THEN** the config load refuses without rewriting it and directs the user to remove the field and install the collection, and the retired CLI options are not available

#### Scenario: Installed server retains two tools

- **WHEN** a harness starts the MCP server installed from the first-party collection
- **THEN** it serves the existing stdio protocol with `search` and `execute` and no extra tools

### Requirement: Complete Marketplace catalog snapshots

Code mode SHALL refresh the catalog from every configured Marketplace endpoint's `/api/v1/catalog`, following cursor pages to completion. It SHALL publish only complete, identity-consistent results for each endpoint; a failed scan SHALL mark the snapshot stale rather than silently omit the endpoint. Cached complete catalogs SHALL be available on later sessions without waiting for a network request.

#### Scenario: Marketplace resources beyond the first page are searchable

- **WHEN** a Marketplace catalog includes a skill and an MCP server after its first page and refresh completes
- **THEN** code-mode search lists both resources using their Marketplace refs, and the MCP server advertises `tools: "unknown"`

#### Scenario: Cold catalog reports stale and later session reuses refresh

- **WHEN** a configured Marketplace has no cached catalog and code mode starts
- **THEN** its initial search reports `catalog.stale`; once the background refresh completes, subsequent search includes its resources, and a later session can read the complete saved catalog before refresh

#### Scenario: Incomplete or inconsistent catalog remains stale

- **WHEN** a Marketplace scan fails, exceeds a bounded scan limit, changes catalog version between pages, returns an invalid cursor, or disagrees with the configured Marketplace instance ID
- **THEN** no partial scan is published as complete, and search reports `catalog.stale` for that endpoint

### Requirement: Marketplace catalog execution

Code mode SHALL expose available Marketplace skill and MCP server refs from completed scans and SHALL retain its verified artifact and assessment gate before using them. Unavailable, revoked, or rejected entries SHALL NOT become executable through a catalog refresh.

#### Scenario: Marketplace skill and declared server execute through the gate

- **WHEN** a script loads an available Marketplace skill and calls a tool on an available declared Marketplace MCP server
- **THEN** the skill content and verified server definition are used, and the session records Marketplace provenance for both

#### Scenario: Unavailable or rejected Marketplace resource is not executable

- **WHEN** a catalog entry is unavailable, revoked, or rejected by its cached assessment
- **THEN** it is not offered as an executable catalog ref, and execution cannot bypass the existing resource gate

### Requirement: JSON-shaped script tool results

Every host value code mode passes to JavaScript, including catalog entries
and their findings, `skills.load` results and their findings, and declared
MCP server tool metadata and results, SHALL use the JSON field names
advertised by code mode rather than Go struct field names or encoded schema
bytes. The `search` and `execute` tool descriptions SHALL declare the shapes
of catalog entries, findings, skill load results, truncation markers, and
thrown errors.

#### Scenario: listTools returns documented JSON fields

- **WHEN** a script calls `listTools()` on a declared MCP server
- **THEN** each tool has lowercase `name`, `description`, and `inputSchema`
  fields, with `inputSchema` as a JSON object

#### Scenario: Tool calls preserve JSON field names

- **WHEN** a declared MCP tool returns a structured result
- **THEN** the script receives its JSON field names rather than Go struct
  field names

#### Scenario: Catalog and skill findings use documented fields

- **WHEN** a script reads `findings` from a catalog entry or from a
  `skills.load` result
- **THEN** each finding has lowercase `message`, `severity`, and `rule`
  fields and no Go-cased fields

#### Scenario: Tool descriptions declare script-visible shapes

- **WHEN** a client lists tools
- **THEN** the `search` and `execute` descriptions declare the finding,
  truncation marker, and thrown error shapes that scripts observe

### Requirement: Catalog ref identity

Every catalog ref SHALL have the form `<registry>/<kind>/<name>`, and each
catalog entry's `registry` field SHALL equal that ref's `<registry>`
prefix. Marketplace resources SHALL use `marketplace:<registry slug>`. A
directory source declared under a v4 source name SHALL use
`local:<source name>`, and a Git source declared under a v4 source name
SHALL use `<source name>`. A source without a configured name SHALL keep a
label derived from its location. When sources with different identities
produce the same label, each SHALL receive a stable digest-derived suffix.
Session entries SHALL retain the ref recorded at load or start, and
`promote` SHALL accept that recorded ref after labels change.

#### Scenario: Ref prefix matches registry field

- **WHEN** a script reads any catalog entry, including a Marketplace entry
- **THEN** its `ref` begins with its `registry` value followed by `/`

#### Scenario: Named sources label refs by source name

- **WHEN** a v4 configuration declares a directory source and a Git source
  under source names that differ from their directory or repository names
- **THEN** their resources appear under `local:<source name>` and
  `<source name>` refs respectively

#### Scenario: Colliding source labels disambiguated

- **WHEN** two configuration layers declare different sources that
  produce the same label
- **THEN** each source's refs carry a distinct suffix that stays the same
  across sessions

#### Scenario: Recorded refs remain promotable

- **WHEN** a session entry recorded a ref under an earlier label and the
  user runs `promote` with that recorded ref
- **THEN** promotion routes through the entry's recorded provenance

### Requirement: Exact Marketplace content access

Online code-mode skill access and declared MCP definition resolution SHALL refresh metadata for the exact catalog resource coordinate and resource revision before using a cached or fetched resource artifact. Validated metadata SHALL be cached for later offline access. A refreshed archive digest SHALL replace an obsolete cached archive digest without advancing the selected resource revision. Artifact bytes SHALL match the archive and content digests in that metadata as well as the artifact's internal digests. Online refresh failures, effective revocation, rejected assessments, and unavailable content SHALL refuse access without falling back to stale metadata or content and without starting a server. Revocation SHALL fail `resource.revoked`, rejection SHALL fail `resource.rejected`, and missing or unavailable content SHALL fail `resource.not-found`. Offline access SHALL use only validated cached metadata and verified artifacts and SHALL NOT perform network requests.

Marketplace skill support files and legacy asset files SHALL share a relative-path namespace. Skill loads SHALL list unique decoded relative paths in sorted order, returning at most 512 paths, and skill reads SHALL return the selected file's bytes only within the existing 4 MiB file limit and traversal/link restrictions. A path present in both file sets SHALL fail `resource.not-found` rather than select one arbitrarily.

#### Scenario: Online Marketplace access refreshes exact revision metadata

- **WHEN** online code-mode access finds cached metadata pointing to an obsolete or missing archive and the Marketplace returns a current archive for the same exact resource revision
- **THEN** access uses and verifies the current archive, caches the validated metadata, and preserves the selected resource revision

#### Scenario: Marketplace skill files include canonical support and legacy assets

- **WHEN** code loads or reads a Marketplace skill with canonical support files or legacy assets, including missing, oversized, traversal, and ambiguous duplicate paths
- **THEN** the load exposes the bounded sorted decoded paths and reads return their exact bytes, while invalid paths or sizes fail without returning bytes and duplicate paths fail `resource.not-found`

#### Scenario: Offline Marketplace access reuses verified exact content

- **WHEN** offline code-mode access requests an exact Marketplace resource revision from its cache, including absent metadata or absent or unverified artifact bytes
- **THEN** validated metadata and verified content are reused without network requests, and absent or unverified inputs fail `resource.not-found`

#### Scenario: Online Marketplace failures refuse stale fallback

- **WHEN** cached authorized content exists but online metadata refresh fails, reports effective revocation or rejection, reports unavailable content, or resolves artifact bytes that disagree with the expected digests
- **THEN** load, read, and declared server execution fail with a coded error, return no stale content, and start no server
