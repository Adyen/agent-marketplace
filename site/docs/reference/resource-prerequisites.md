# Resource prerequisites

A resource can declare `metadata.requires` in its front matter to name other
resources it needs. Prerequisites are advisory: an unmet prerequisite produces
a warning with a suggested install command and never changes the exit status
of any command.

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

`collection` is not a supported prerequisite kind, and an empty or missing
`kind` means `skill`. `kind: all` also resolves to a skill prerequisite
today; do not rely on that spelling, it is not a supported way to require
every kind.

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

An item that cannot be read is skipped: the resource still loads, the
remaining items still apply, and nothing is printed in command output. The
CLI records the skip only in its log file (visible with `--debug`). An item
is skipped when:

- `metadata.requires` itself is not a list;
- the item is neither a string nor a map;
- `name` is missing, blank, or not a string;
- `kind`, `registry`, or `path` is present but not a string;
- `kind` names an unknown kind or `collection`.

## Satisfaction

A prerequisite is met when a selected resource in the same resolution has the
same kind and name, regardless of which registry, path, or configuration
layer supplies it. The `registry` and `path` hints do not restrict
satisfaction — they only shape the remediation command. A skill named `docs`
does not satisfy an `mcp:docs` prerequisite.

## Warning and remediation

An unmet prerequisite produces:

```text
warning: <kind> "<resource>" requires <kind> "<prerequisite>", but it is not selected
  → agent-marketplace install "<prerequisite>" --kind <kind>
```

The remediation gains `--from "<registry>"` and `--path "<path>"` when the
item declares those hints. Kinds print as `skill`, `command`, `subagent`,
`hook`, or `mcp`. URL credentials inside a `registry` hint are replaced with
`***`.

| Command | Behavior |
|---|---|
| `install` | Prints warnings only for the resources the install selected, including members of a collection it installs. |
| `sync` | Prints grouped warnings. |
| `sync --check` | Prints grouped warnings. |
| `status` | Prints an `UNMET REQUIREMENTS` table with `RESOURCE`, `REQUIRES` (`<kind>:<name>`), and `FIX` columns, sorted by `RESOURCE` then `REQUIRES`. |
| `doctor` | Prints grouped warnings. |

## Exit status

Unmet prerequisites are warnings only. `install`, `sync`, `sync --check`,
`status`, and `doctor` exit with status 0 when unmet prerequisites are the
only findings, and `sync --strict` does not promote them to errors either —
`--strict` applies only to unresolved-selection warnings.

## Related documentation

- [How to declare resource prerequisites](../how-to/author/declare-resource-prerequisites.md)
- [Resource metadata](./resource-metadata.md)
- [Registry layout](./registry-layout.md)
