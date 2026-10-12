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

## Warning and remediation

An unmet prerequisite produces:

```text
warning: <kind> "<resource>" requires <kind> "<prerequisite>", but it is not selected
  → agent-marketplace install "<prerequisite>" --kind <kind>
```

The remediation gains `--from "<registry>"` and `--path "<path>"` when the
item declares those hints. Kinds print as `skill`, `command`, `subagent`,
`hook`, or `mcp`. URL credentials inside a `registry` hint are replaced with
`***`. Internally each unmet warning also carries a typed reason —
`not-found`, `excluded`, or `source-not-configured` — which does not change
the printed text or table columns.

| Command | Behavior |
|---|---|
| `install` | Prints warnings only for the resources the install selected, including members of a collection it installs. |
| `sync` | Prints grouped warnings. |
| `sync --check` | Prints grouped warnings. |
| `status` | Prints an `UNMET REQUIREMENTS` table with `RESOURCE`, `REQUIRES` (`<kind>:<name>`), and `FIX` columns, sorted by `RESOURCE` then `REQUIRES`. |
| `doctor` | Prints grouped warnings. |

## Exit status

`install`, `sync`, `sync --check`, `status`, and `doctor` exit with status 0
when unmet prerequisites are the only findings. `sync --strict` — including
`sync --strict --check` — still prints the advisory warning and remediation,
but exits nonzero so CI can enforce declared prerequisites. Strict mode never
selects the prerequisite for you.

## Related documentation

- [How to declare resource prerequisites](../how-to/author/declare-resource-prerequisites.md)
- [Resource metadata](./resource-metadata.md)
- [Registry layout](./registry-layout.md)
