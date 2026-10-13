# Resource prerequisites

A resource can declare `metadata.requires` in its front matter to name other
resources it needs. Prerequisites are advisory: an unmet prerequisite produces
a warning with a suggested install command. Only `sync --strict` (including
`sync --strict --check`) fails on an unmet prerequisite; every other command
keeps it advisory.

Skills, commands, and subagents written as Markdown with front matter read
`metadata.requires` from the `metadata` map. Hooks and MCP servers have no
documented front matter, so they cannot declare prerequisites.

## `requires` item

`metadata.requires` is a list. Each item is either a map or a string
shorthand. Surrounding whitespace is trimmed from every value.

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | required | Required resource name. An item without a non-empty string `name` is skipped. |
| `kind` | string | `skill` | Required resource kind. Matched case-insensitively; see the spellings below. |
| `registry` | string | none | Registry hint, used as `--from` in the suggested install command. |
| `path` | string | none | Path hint, used as `--path` in the suggested install command. |

### Kind spellings

`kind` accepts the same singular, plural, and alias spellings as the `--kind`
flag:

| Kind | Accepted spellings |
|---|---|
| skill | `skill`, `skills` |
| command | `command`, `commands` |
| subagent | `subagent`, `subagents`, `agent`, `agents` |
| hook | `hook`, `hooks` |
| mcp | `mcp`, `mcpserver`, `mcpservers`, `mcp_server`, `mcp_servers` |

`collection` and `all` are not supported prerequisite kinds, and an empty or
missing `kind` means `skill`. `all` is a `--kind` filter, not a resource kind,
so it is rejected in prerequisite declarations rather than read as a skill.

### Shorthand

A string item is `kind:name` for a non-skill prerequisite, or a bare `name`
for a skill prerequisite:

```yaml
metadata:
  requires:
    - mcp:docs-server   # kind mcp, name docs-server
    - other-skill       # kind skill, name other-skill
```

## Malformed items

An item that cannot be read is skipped: the resource still loads and the
remaining items still apply. `list --from` and `doctor` print a warning with
the stable code `catalog.requires.invalid`, the declaring resource, its
source file, and the item index when the malformed value is a list entry;
the warning never quotes the malformed value. An item is skipped when:

- `metadata.requires` itself is not a list;
- the item is neither a string nor a map;
- `name` is missing, blank, or not a string;
- `kind`, `registry`, or `path` is present but not a string;
- `kind` names an unknown kind, `all`, or `collection`.

## Satisfaction

A prerequisite is met when a selected resource in the same resolution has the
same kind and name, regardless of which registry, path, or configuration
layer supplies it. The `registry` and `path` hints do not restrict
satisfaction — they shape the remediation command and steer lookup when
[requirement expansion](#requirement-expansion) is enabled. A skill named
`docs` does not satisfy an `mcp:docs` prerequisite.

## Requirement expansion

Expansion is opt-in and off by default. Set `requires.expand: true` in any v4
configuration layer to turn it on:

```yaml
requires:
  expand: true
```

The highest-precedence layer that sets the value wins, an explicit `false`
overrides a lower-precedence `true`, and a layer that omits it changes
nothing. v2 and v3 configurations keep the release default of `false`, and
`migrate` does not add the key. See the
[configuration reference](configuration.md) and
[configuration layers reference](configuration-layers.md#requires).

When expansion is enabled, a prerequisite that is not already selected is
looked up and selected transitively after collections are expanded:

- Without a `registry` hint, only the requiring resource's own source (same
  registry, ref, and root) is searched.
- A `registry` hint must match a source that is already configured: a Git
  source by its normalized URL, with `path` narrowing to a matching
  configured root, or a directory source by its absolute location. A hint
  that matches nothing configured produces a warning; the CLI never fetches
  or guesses another source.
- Implied resources are selected only in the requiring resource's scope and
  only for the supplying group's harness targets. They are not `required`,
  and a direct selection wins a collision with an implied candidate.
- Explicit exclusions still win unless a `required: true` group supplies
  the resource. An exclusion in the repository scope cannot unselect a
  machine-scope resource.
- Requirement cycles terminate, and the first candidate in declaration and
  resolution order wins among competing implied candidates.

With expansion disabled, `sync` computes the same transitive closure as a
preview only. Selections are unchanged, but outputs the preview would
produce are preserved when they already exist with matching ownership:
`sync`, `sync --locked`, and `sync --check` never create, rewrite, or adopt
files for a previewed resource, and they do not prune an existing managed
output that a preview still covers. Removing the last resource that
requires it ends that retention at the next sync.

Expansion also changes one `remove` outcome. With expansion enabled,
removing an optional resource that a still-selected resource requires
succeeds: `remove` drops the resource's direct selection when one exists
and records an explicit exclusion, so the requiring resource stays
selected and reports the prerequisite as unmet. Removal never cascades to
the requiring resource and needs no new flag. With expansion disabled,
`remove` behaves as before and records no prerequisite-driven exclusion.

### Marketplace prerequisites

Prerequisites declared by Marketplace resources keep their recorded edges
in the acquisition lock: an entry in a record's `selected_by` carrying a
`kind` is a requires edge naming the requiring locked resource. A record
held only through requires edges is not a user selection and never counts
as a collection member. When a still-selected parent declares its
prerequisite, a requires-held resource joins the selection as an implied
resource (expansion enabled) or the implied preview (disabled); an edge
with no such parent leaves the record unselected and unreported.

For a prerequisite with no recorded edge, lookup stays inside the
requiring resource's configured Marketplace entry:

- Without a `registry` hint, the requiring pin's own registry is searched.
- A `registry` hint must name a registry bound in that entry by slug or
  registry ID; an unbound hint produces a `source-not-configured` warning
  and no Marketplace request.
- When the requiring pin records `registry_commit` and the target is its
  own registry, only that exact registry occurrence is consulted — through
  its verified registry revision manifest — with no fallback to the
  current revision. Otherwise the target registry's current revision is
  used.
- A `path` hint matches only when the candidate's source root — the single
  root of its exact registry occurrence containing its source path —
  equals the hint; when the root cannot be established the prerequisite
  is `not-found`.
- Offline resolution never contacts a Marketplace for lookup.

This release never records a lookup result: a found candidate appears in
the implied preview only, preserves or prunes no materialized output,
changes neither configuration nor lock bytes, and — with expansion
enabled — reports the prerequisite unmet with reason `unrecorded`.

Removing a requires-held prerequisite succeeds like any authorized
removal: its lock record and requires edges are removed, an exact
`registry/kind/name` Marketplace exclusion is written only when expansion
is enabled (into the user layer, the one layer where Marketplace
exclusions take effect), and the requiring resource stays selected with
its prerequisite reported unmet.

## Warning and remediation

An unmet prerequisite produces:

```text
warning: <kind> "<resource>" requires <kind> "<prerequisite>", but it is not selected
  → agent-marketplace install <prerequisite> --kind <kind>
```

The remediation gains `--from <registry>` and `--path <path>` when the item
declares those hints. Each argument is a POSIX shell literal: simple values
print unquoted and values that need quoting are single-quoted. Kinds print as
`skill`, `command`, `subagent`, `hook`, or `mcp`. URL credentials inside a
`registry` hint are replaced with `***` before the command is formatted. Each
unmet warning also carries a typed reason — `not-found`, `excluded`,
`source-not-configured`, or `unrecorded` — which `status` prints in its `REASON` column and
`doctor` uses to explain unconfigured source hints.

| Command | Behavior |
|---|---|
| `install` | Prints warnings only for the resources the install selected, including members of a collection it installs. After its summary it also prints `also selected <kind> "<name>" (required by <kind> "<name>")` for each prerequisite it selected transitively when [requirement expansion](#requirement-expansion) is enabled, or `would also select` for the eligible preview when expansion is disabled. |
| `sync` | Prints grouped warnings. |
| `sync --check` | Prints grouped warnings. |
| `status` | Prints an `UNMET REQUIREMENTS` table with `RESOURCE`, `REQUIRES` (`<kind>:<name>`), `REASON`, and `FIX` columns, sorted by `RESOURCE` then `REQUIRES`. Its main table's `VIA` column shows `requires <kind>:<name>` provenance alongside collection provenance. |
| `doctor` | Prints grouped warnings, and explicitly reports `unconfigured prerequisite source` for an unmet prerequisite whose `registry` hint matches no configured source. That line is warning-only: `doctor` never configures or fetches the hinted source. |

## Exit status

`install`, `sync`, `sync --check`, `status`, and `doctor` exit with status 0
when unmet prerequisites are the only findings. `sync --strict` — including
`sync --strict --check` — still prints the advisory warning and remediation,
but exits nonzero so CI can enforce declared prerequisites. Strict mode changes
only the exit status; whether a prerequisite is selected depends solely on
[requirement expansion](#requirement-expansion).

## Related documentation

- [How to declare resource prerequisites](../how-to/author/declare-resource-prerequisites.md)
- [Resource metadata](./resource-metadata.md)
- [Registry layout](./registry-layout.md)
