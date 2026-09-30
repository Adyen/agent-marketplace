## MODIFIED Requirements

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

## ADDED Requirements

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
