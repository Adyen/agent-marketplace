# Code-mode MCP from a Marketplace

**Status:** Design for review

**Date:** 2026-09-25

**Origin:** Brainstorming session on applying [Cloudflare Code Mode](https://blog.cloudflare.com/code-mode-mcp/) to agent-marketplace

## Objective

Let an agent discover and use Marketplace resources dynamically instead of receiving them only through static materialization. A local MCP server, `agent-marketplace mcp`, exposes a fixed two-tool surface (`search` and `execute`) whose context cost stays constant regardless of catalog size. Model-written JavaScript runs in a sandbox against a read-only catalog, on-demand skill content, and lazily started catalog MCP servers.

Static materialization through `sync` remains unchanged and remains the only path for hooks, subagents, commands, and rules.

## Decisions

| Decision | Choice | Rejected alternatives |
|---|---|---|
| Scope | Skills on demand, code-mode catalog search, and MCP gateway, designed together and delivered in phases | Designing one slice alone |
| Runtime location | Local only: `agent-marketplace mcp` stdio server started by the harness | Hosted `serve /mcp` (requires executing resources, authentication, and tenant isolation, all current serve non-goals); hybrid with hosted read-only discovery |
| Process management | Opt-in: once enabled in a config layer, `sync` registers the server as an owned MCP entry in every selected harness; the harness owns its lifecycle | User-managed daemon; automatic registration by `init` for all users or whenever a Marketplace endpoint is configured |
| Dynamic reach | Anything on configured endpoints and registries, gated by assessment and active revocation | A config `dynamic:` scope; pre-selected resources only |
| Sandbox | goja, in-process, one VM per call | QuickJS on wazero (hard memory isolation, needed only for multi-tenant hosting, which is out of scope); Starlark (no exceptions or async); structured search without code |
| MCP tool schemas | Lazy local `tools/list`, cached by server-definition digest | Publisher-declared tool manifests; both |
| Server start gate | Harness approval of the `execute` call, strengthened by an explicit `servers` parameter and a `search` that never starts processes | Per-digest elicitation consent; installed servers only; assessment only |

Organization control over dynamic reach comes from the organization config layer choosing which endpoints and registries are configured. Per-resource control comes from assessments and revocation.

## Architecture

```text
 harness (Claude, Codex, Devin, ...)
      |  stdio MCP, started and stopped by the harness
      v
 agent-marketplace mcp
   |-- search({code})            goja VM, read-only catalog binding
   |-- execute({servers, code})  goja VM, catalog + skills.load + mcp.<alias>
   |
   |-- internal/codemode    VM lifecycle, limits, bindings, error mapping
   |-- internal/mcpclient   stdio/HTTP MCP client, session server pool, schema cache
   |-- existing: pipeline.LoadEffective, marketplace search/content/assessment/revocation,
   |             registry cache, ownership-reconciled harness MCP entries
   v
 Marketplace serve API (unchanged)        Git / directory registry caches
```

### Tool surface

The complete tool list presented to the model:

- `search({code})` runs an async arrow function against the `catalog` binding. It never starts a process and never writes anything.
- `execute({servers, code})` runs an async arrow function with the `catalog`, `skills`, and `mcp` bindings. `servers` is an array of exact MCP server refs; only those servers are bound under `mcp` and only they may start.

Tool descriptions state the binding shapes compactly. The target footprint is about 1,000 tokens.

### Bindings

`catalog` is an in-memory snapshot merging every configured Marketplace endpoint and every configured Git and directory registry. Each resource:

```ts
{
  ref: string,            // e.g. "public:acme/skill/pdf"
  kind: "skill" | "command" | "subagent" | "hook" | "mcp" | "collection",
  name: string,
  description: string,
  tags: string[],
  registry: string,
  revision: string,
  assessment: "allow" | "warn",
  findings?: Finding[],
  tools?: ToolSchema[] | "unknown"   // MCP servers only; "unknown" until listed once
}
```

Resources with a `reject` assessment or an active revocation are omitted. Non-skill, non-MCP kinds are visible for discovery but cannot be loaded or started through this server.

`skills.load(ref)` returns `{ ref, revision, digest, content, files, findings? }`, where `content` is `SKILL.md` and `files` lists the skill's other files. `skills.read(ref, path)` returns one file. Content is fetched through the existing content and artifact endpoints (or the registry cache), verified by digest, and stored in the regular cache.

`mcp.<alias>.<tool>(args)` calls a tool on a declared server, and `mcp.<alias>.listTools()` returns its schemas. `<alias>` is the resource name, disambiguated with a registry prefix when two declared servers share a name.

### Local components

- `internal/codemode`: creates one goja VM per call; enforces a deadline with `vm.Interrupt()` (default 30 s), an output-size cap, and a host-call cap; exposes no globals other than the bindings; maps script failures to stable error codes.
- `internal/mcpclient`: a stdio and HTTP MCP client. Keeps one pool of started servers per stdio session, stopped when the session ends. Tool schemas are cached at `~/.cache/agent-marketplace/mcp-tools/<definition-digest>.json`.
- `cmd/agent-marketplace/mcp.go`: the Cobra command. Loads config through `pipeline.LoadEffective` in cached mode at startup, then refreshes online in the background.
- Harness registration is opt-in and never automatic. `agent-marketplace init --code-mode` or `agent-marketplace mcp enable [--scope user|repo|local]` sets `codeMode: { enabled: true }` in the chosen v4 config layer; `mcp disable` removes it. While the effective config enables it, `sync` adds an owned `agent-marketplace` MCP entry through the existing aggregate-ownership paths, and removes that entry when it is disabled. The entry follows the same ownership, backup, and trust-review rules as any other managed MCP entry, including `materialize.codex.trust-review`.
- `agent-marketplace promote <session|ref>`: converts a session-log entry into a normal installed, locked selection using the existing install path.

### Session log

Every loaded skill and every started server is appended to a session log under `$XDG_STATE_HOME/agent-marketplace/mcp-sessions/`, recording the exact ref, revision, and digest. The log is evidence for `promote` and for audit; it is not a lockfile and does not affect `sync`.

## Data flow

### Startup

1. The harness starts `agent-marketplace mcp`.
2. The server loads the effective config in cached mode and builds the catalog snapshot from the existing Marketplace catalog cache and registry caches.
3. It answers `tools/list` immediately with `search` and `execute`.
4. An online refresh runs in the background and atomically replaces the snapshot. Without network access the server stays usable, and results include a `catalog.stale` warning.

### search

1. A new VM is created with the current snapshot bound as `catalog`.
2. The model code runs until it returns, throws, or hits a limit.
3. The return value is serialized as JSON. Output beyond the cap is truncated and marked `truncated: true`.

### execute

1. Every `servers` ref resolves to an exact revision and definition digest. A revoked, rejected, or unknown ref fails the whole call before any code runs.
2. A new VM is created with `catalog`, `skills`, and `mcp` bound only for the declared servers.
3. The first `mcp.<alias>` use starts the server with only the environment variables its definition declares, runs the initialize handshake, lists tools, and writes the schema cache.
4. `skills.load` and `skills.read` fetch, verify, and cache content.
5. The return value goes back to the model. Each skill load and server start is appended to the session log.

## Error handling

Errors return as MCP tool results with `isError: true` and a stable code. The server process never exits because of a tool call.

| Code | Meaning |
|---|---|
| `codemode.timeout` | VM deadline reached |
| `codemode.output-limit` | Output cap reached |
| `codemode.host-call-limit` | Host-call cap reached |
| `codemode.script-error` | Model script failed; JavaScript stack included with host paths removed |
| `resource.revoked` | Active revocation |
| `resource.rejected` | Assessment decision is `reject` |
| `resource.not-found` | No resource matches the ref |
| `resource.unsupported-kind` | Load or start requested for a kind this server does not serve |
| `mcp.not-declared` | Code referenced a server not listed in `servers` |
| `mcp.start-failed` | Server failed to start; first 2 KB of stderr with secrets removed |
| `mcp.tool-error` | Error passed through from the server |
| `catalog.stale` | Warning: snapshot is old or the network is unavailable |

Inside `execute`, a failed `mcp.*` call throws a catchable JavaScript error. A server that hangs is killed at the VM deadline. A server that crashes is marked dead for the rest of the session and is not restarted automatically.

## Security model

- The sandbox exposes only the bindings. There is no filesystem, network, process, or environment access from model code.
- Code runs in the user's own harness-started process, so resource exhaustion affects only that user. goja has no hard memory limit; the deadline and output caps bound the realistic cases.
- Starting a catalog MCP server runs registry code with the user's privileges. The gate is the harness's approval of the `execute` call. The explicit `servers` parameter makes that approval show exactly which servers will start, and `search` never starts a process, so an auto-approved read-only tool cannot run registry code.
- Started servers receive only the environment variables their definitions declare.
- Loaded skill content is untrusted prompt input. `warn` findings are returned alongside the content so the model and user can see them.
- Logs record tool names, durations, gate decisions, server starts and exits, and cache hits. Code bodies, tool arguments, and skill content are never logged at info level.

## Delivery phases

1. **P1: goja feasibility spike (throwaway).** Measure VM creation cost, `vm.Interrupt()` responsiveness, async/await host-call support, and binary size growth. Pass criteria: VM creation under 5 ms, an interrupted script stops within 100 ms, and `await` on a host function works. If the spike fails, revisit the sandbox decision before P2.
2. **P2: Skills on demand.** `agent-marketplace mcp` with `search`, and `execute` limited to `catalog` and `skills`. Gates, session log, the `codeMode` config key with opt-in harness registration, and `promote`.
3. **P3: MCP gateway.** `internal/mcpclient`, the `servers` parameter, lazy start, schema cache, environment filtering, and the session server pool.
4. **P4: Ergonomics.** Accept TypeScript by stripping types with esbuild, and describe the bindings with generated `.d.ts` text in tool descriptions.

## Testing

- `internal/codemode` unit tests: timeout, output cap, host-call cap, absence of non-binding globals, error code mapping, rejection of undeclared `mcp` aliases.
- `internal/mcpclient` unit tests against a fake stdio MCP server helper binary: initialize, `tools/list`, tool calls, crash, hang, and environment filtering.
- Gate tests: revoked and rejected skills and servers are blocked before code runs; `warn` findings are returned.
- Command tests in `cmd/agent-marketplace` (with the isolation guard): a full stdio MCP session over pipes.
- Standalone E2E: seed a registry with a skill and a fake MCP server; confirm `sync` adds no entry by default; run `mcp enable`, then confirm `sync` registers the entry for Claude, Codex, and Devin and `mcp disable` plus `sync` removes it; run `search`, then `execute` with `skills.load` and one tool call; assert the session log; `promote` and assert the config and lock.
- Benchmarks: `BenchmarkSearchCatalog` at 1k and 10k resources, `BenchmarkExecuteVMStartup`, `BenchmarkToolSchemaCacheLookup`.

## Non-goals

- Hosted `/mcp` on `serve`, or any code execution in `serve`
- Proxying HTTP MCP servers through `serve`
- Dynamic loading of hooks, subagents, commands, or rules
- Persistent per-server consent state
- Automatic restart of crashed servers
- A config `dynamic:` scope limiting reach below the configured endpoints

## Open questions for implementation planning

- Exact default values for the output cap and host-call cap.
- Whether the organization layer may force-disable `codeMode` for lower layers.
