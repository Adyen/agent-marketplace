# Claude Enterprise delivery

## Context

The Marketplace service already ingests registries, analyzes resources against
policy, stores a canonical artifact per resource revision, and publishes a
`RegistryPublication` holding resource occurrences and assessments. Clients pull
from it. Nothing pushes.

Claude Desktop reads plugins from the member's claude.ai account. On Claude
Enterprise, an Admin API key can write that account state for the whole
organization through the documented Plugins API. This change makes the service a
publisher to that API.

Verified against the Plugins API documentation (`anthropic-version: 2026-10-21`,
`anthropic-beta: ce-plugins-2026-09-01`) on 2026-11-11.

## Goals

- Members on Claude Desktop, Cowork, and Claude Code see Marketplace resources in
  **Customize > Plugins** without installing anything and without a second sign-in.
- One organization credential, held by the service, instead of one browser session
  per member.
- Only content that passed policy and is not revoked ever leaves the service.
- Reconciliation is idempotent, resumable, and cheap when nothing changed.

## Non-goals

- Member-owned (personal **My Uploads**) plugins. The API returns `403` for uploads
  to a member's plugin, and there is no documented user-scoped API. Issue #960's
  browser-profile approach is explicitly dropped.
- Pinning, promotion, and rollback of served versions (see D6).
- Deleting plugins. Deletion is permanent and has no per-version form; operators
  delete in claude.ai when they must.
- Collections as plugins (see D2).
- Git-marketplace synchronization. A `github`/`gitlab` marketplace accepts no
  uploads, so it is an alternative delivery route, not a complement to this one.
- Writing to Claude Desktop's local application directories. Those paths are a
  private cache, not a supported input.
- Non-Enterprise plans. The API returns `400` for other organization types.

## Decisions

### D1. Push through the Plugins API, not a Git marketplace

The two supported ways into an organization's plugin list are a `manual`
marketplace fed by uploads and a `github`/`gitlab` marketplace synchronized from a
repository. A synchronized marketplace accepts no uploads and cannot have its
plugins deleted through the API, because the next synchronization would undo the
change.

The service already holds the publication state and the policy verdicts, and it is
not a Git host. Pushing to a `manual` marketplace keeps one source of truth and
avoids giving the service write access to a repository that members' Claude
organizations then trust. The Git route stays available to operators who prefer it
and is unaffected by this change.

### D2. One plugin per resource

A Claude plugin is the unit a member installs. Mirroring one plugin per resource
keeps the granularity the Marketplace already has: a member installs the one skill
they want, not a bundle.

The alternative, one plugin per collection or per registry, gives a shorter list
but takes away per-resource choice and makes a single resource change republish a
large archive. Per-resource mirroring is odd in one respect, a plugin that holds a
single hook or MCP server is a thin wrapper, but the Claude plugin format allows it
and the member-facing result is the correct one.

Costs that this choice accepts, and how they are bounded:

- **Marketplace capacity.** A marketplace holds 1,000 plugins, counting plugins the
  API does not list. The reconciler reports capacity use in its status output and
  records a stable code when a create fails for capacity, so an operator sees the
  ceiling before it is hit.
- **Write rate.** Writes are limited to 60 a minute for the organization. A first
  full mirror of N resources costs N creates plus N installation-setting writes;
  steady state costs one upload per changed resource. The reconciler paces itself
  and resumes on the next pass, so a large first mirror spans several passes rather
  than failing.
- **Member-facing list length.** Members see one entry per resource. This matches
  the Marketplace browser and is the behavior being asked for.

Collections are virtual and would have to be mirrored as plugins that duplicate
their members' content, which would install the same skill twice under two plugins.
They are out of scope until there is a reason to accept that duplication.

### D3. The service never guesses the marketplace

When delivery is enabled, the reconciler resolves the target marketplace once per
pass with `GET /v1/organizations/plugin_marketplaces` and refuses to run unless it
is organization-owned with source `manual`. An operator may name one with
`marketplace_id`; omitting it uses the organization's library marketplace, which is
created on first upload.

A dedicated marketplace is recommended and documented, because the library
marketplace is shared with administrator uploads and enforces `skill_name_taken`
against organization-level skills. Whether claude.ai lets an administrator create a
second `manual` marketplace is an operator-facing unknown, so the configuration
supports both and the first task in the change is to verify it against the real
organization.

### D4. Names are derived, stable, and namespaced

`name` in `.claude-plugin/plugin.json` must be lowercase letters, digits, and
hyphens, at most 64 characters, and unique within the marketplace. The reconciler
derives it from the resource coordinate:

    <prefix>-<registry-slug>-<kind>-<resource-name>

lowercased, with every other character folded to `-`, repeats collapsed, and ends
trimmed. When the result exceeds 64 characters it is truncated to 55 and suffixed
with `-` plus the first 8 hex characters of the SHA-256 of the full logical key, so
two long names cannot collide.

The derived name is written to the delivery state row on first publish and is never
recomputed for an existing plugin, so a slug change in server configuration cannot
orphan a live plugin. The default prefix `am` keeps mirrored plugins distinguishable
from administrator uploads in a shared marketplace.

The inner resource keeps its own name: a skill mirrored as plugin
`am-adyen-skill-threat-model` still presents the skill `threat-model` to members.

### D5. Build from the published artifact, validate locally, upload only on change

The archive is built from the stored canonical artifact, which is already the exact
bytes the publication verified: `artifact.DecodeResource` reconstructs the
`ir.Resource`, and the artifact's payload, assets, licenses, and support files are
placed into the Claude plugin source layout by kind:

| Kind | Path in the plugin |
| --- | --- |
| skill | `skills/<name>/SKILL.md` plus assets |
| command | `commands/<name>.md` |
| subagent | `agents/<name>.md` |
| hook | `hooks/hooks.json` plus support files under `hooks/<name>/` |
| mcpserver | `.mcp.json` |

The manifest is generated: `name` from D4, `displayName` and `description` from the
resource (bounded to 64 and 500 characters), `author` and `homepage` from the
registry publisher. `version` is written only when the resource declares one;
provenance goes in `release_notes`, which claude.ai shows in version history, as
the publication ID, the registry commit, and the content digest.

Every documented upload requirement is checked locally before a request is built:
exactly one manifest, name charset and length, `SKILL.md` frontmatter with `name`
and `description` free of XML tags, no duplicate skill or command names, no
top-level `bin/`, no nested `.zip`, relative paths within the allowed character set
and depth, the 5,000-file and 200 MB ceilings, and DEFLATE or STORE entries with no
symbolic links. A resource that fails is skipped with a stable code and never
uploaded: an invalid archive would otherwise burn write quota to earn a `400`.

Archives are deterministic (sorted entries, fixed timestamps and permissions), so
the archive digest is a change signal. The API deduplicates nothing: uploading
identical bytes stores another version. Change detection therefore lives in our
state, keyed on the archive digest.

### D6. The served version follows the latest upload

Plugins are left unpinned. A new upload becomes the served version as soon as it is
stored, which is the simplest correct behavior and needs no promotion step.

The documented consequence is accepted and must be stated in operator
documentation: while organization content scanning is on, a version uploaded
through the API never waits for its scan, so **members lose access to the plugin
from the moment the new version is stored until its scan completes with `pass` or
`warn`**. The reconciler records `content_scan.status` per plugin and reports it, so
an operator can see a plugin that is withheld. Recovery from a bad revision is to
publish a corrected one, not to roll back.

Pinning would avoid the gap by keeping members on the previous version until a scan
passes, at the cost of a promotion step, a scan-polling loop, and the rule that a
pinned plugin can never be unpinned. That is a later change if the scan gap proves
disruptive.

### D7. Availability is set per plugin, never on the marketplace

A plugin created through the API starts with no setting of its own and inherits its
marketplace default, which is `not_available` unless someone changed it. The
reconciler therefore sets the organization-wide setting explicitly to the configured
preference (`available` by default) right after creating a plugin.

Writing the marketplace default instead would be one call rather than one per
plugin, but it would also change the visibility of every plugin administrators
uploaded into that marketplace by hand. The service does not touch state it does not
own.

Installation-setting writes for one plugin are sent one at a time, because
concurrent writes for the same plugin can be answered with `503`.

### D8. Withdrawal is availability-only, and honest about it

When a resource stops being published, is rejected by policy, or is revoked, its
plugin's organization-wide setting is set to `not_available` and the state row is
marked retired. The plugin and its version history remain in the organization.

This must not be described as a recall. Members who already installed the plugin
keep what their machines downloaded, and the uploaded archive stays in the
organization's version history until an operator deletes the plugin in claude.ai.
The status output says exactly this, so nobody treats a revocation as content
removal.

If the resource returns, the setting goes back to the configured preference and the
row is republished; the existing plugin and its history are reused.

### D9. Adoption is proof-based

Before creating a plugin the reconciler reads the marketplace inventory. A plugin
that already holds the derived name is adopted only when its `created_by` is an
`api_actor` carrying our key's `api_key_id`. Anything else, an administrator's
plugin, another integration's, or a plugin with no recorded creator, is left alone
and recorded as a conflict with a stable code.

This mirrors the ownership discipline the client uses for shared harness files:
never write to an entry we cannot prove we own.

### D10. Retries follow the documented rules exactly

No endpoint accepts an idempotency key, so each failure mode is handled as
documented:

- `409 plugin_name_taken` on create: read the named plugin; adopt it under D9 or
  record a conflict. Never upload into a plugin we cannot prove is ours.
- `503 registration_pending` on create: the plugin **was** created and
  `x-should-retry` is `false`. Do not resend the create; upload the same files as a
  version of `details.plugin_id`.
- `503 registration_pending` on a version create: the version **was** stored.
  Resend only when `x-should-retry` is `true`.
- A lost response on a version create: the plugin's `latest_version_id` is recorded
  before every upload, so the reconciler re-reads the plugin and retries only when
  it is unchanged. This is why the state row keeps that ID.
- `429`: honour `retry-after` and resume on the next pass.
- `skill_name_taken`: skip the resource with a stable code naming the conflicting
  skill. It is an operator decision, not something to resolve by renaming silently.

Unrecognized enum values (component types, `reach`, scan status, marketplace source,
error codes) are treated as opaque strings, never as failures.

## Architecture

    registry refresh ──> publication ──┐
    revocation change ─────────────────┼──> enqueue claude-delivery job
    repair interval ───────────────────┤
    operator --sync-once ──────────────┘
                                        │
                                        v
      claudedelivery.Reconcile
        1. resolve config + credential + marketplace (preflight)
        2. desired set  := allowed, unrevoked resource revisions of current publications
        3. inventory    := GET /v1/organizations/plugins?marketplace_id=...
        4. per resource : build -> validate -> compare digest -> create | version | skip
                          create -> POST installation_settings/organization
        5. per orphan   : POST installation_settings/organization = not_available
        6. record state, metrics, sanitized failures

New packages:

- `pkg/marketplace/claudeplugins` — typed Plugins API client. Owns headers,
  pagination, rate-limit handling, `x-should-retry`, typed error codes, and
  credential redaction. No business logic.
- `pkg/marketplace/claudedelivery` — archive assembly, local validation,
  the reconciler, and the desired-set query. Pure where it can be: assembly and
  validation take bytes and return bytes and diagnostics.
- `pkg/marketplace/store` — `claude_delivery_plugins`, one row per mirrored
  resource lineage: registry ID, kind, resource name, derived plugin name,
  `plugin_id`, `marketplace_id`, published archive and content digests,
  `published_version_id`, `latest_version_id`, `served_version_id`,
  `content_scan_status`, `reach`, installation preference, state
  (`published`, `retired`, `skipped`, `conflict`, `failed`), sanitized last error
  code and message, and timestamps.
- `pkg/marketplace/worker/claude_delivery.go` — job kind `claude-delivery`, leased
  and retried like managed jobs, one in flight per instance.

## Security

- The Admin API key is referenced by file or environment variable only, never
  inline in configuration, never stored in the database, never logged, and never
  returned by any API or CLI surface. It is registered with the existing redaction
  helper.
- Required scopes are `write:plugins` and `read:plugins`. The key is organization-
  scoped and keeps working after its creator leaves, so operator documentation
  covers rotation.
- Only resource revisions whose assessment allows them and which carry no revocation
  are eligible. Rejected and revoked content is never built, never uploaded, and is
  actively withdrawn.
- Logs, metrics, and status output carry identity, codes, and counts only: never
  resource bodies, hook commands, MCP environment values, or request bodies, which
  matches the existing `SkippedResource` discipline.
- Every write is attributed to the key on the organization's Activity Feed, giving
  the operator an independent audit trail.
- Delivery is off unless configured. A disabled target makes no outbound request.

## Risks

| Risk | Handling |
| --- | --- |
| Content scanning withholds a plugin after each update (D6) | Record and report scan status; document the gap; pinning is a known follow-up |
| A second `manual` marketplace may not be creatable in claude.ai (D3) | Verified in task 0 against the real organization before the configuration shape is frozen |
| 1,000-plugin marketplace limit with per-resource granularity (D2) | Capacity reported in status; a capacity refusal has its own code |
| 60 writes a minute throttles a large first mirror | Pacing plus resumable passes; progress is recorded per plugin |
| The beta header or endpoint shape changes | One client package, one place to change; unknown values are tolerated by contract |
| An operator points two services at one marketplace | Adoption requires our own `api_key_id` (D9); foreign plugins are reported, never written |
