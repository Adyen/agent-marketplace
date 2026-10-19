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
satisfaction; they only shape the remediation command. A skill named `docs`
does not satisfy an `mcp:docs` prerequisite.

## Warning and remediation

An unmet prerequisite produces:

```text
warning: <kind> "<resource>" requires <kind> "<prerequisite>", but it is not selected
  → agent-marketplace install <prerequisite> --kind <kind>
```

The remediation gains `--from <registry>` and `--path <path>` when the item
declares those hints. Kinds print as `skill`, `command`, `subagent`, `hook`,
or `mcp`. URL credentials inside the `registry` and `path` hints are
replaced with `***` before the argument is formatted.

Each remediation argument is a literal POSIX shell argument. A value made
only of letters, digits, and `._/:@%+,-=` prints bare; any other value,
including one containing whitespace, shell substitution, or embedded single
quotes, is wrapped in single quotes with each embedded quote escaped as
`'\''`. The quoting is display formatting only; the resource name keeps
whatever characters the source authored, and the characters shown can always
be pasted literally into a shell. The warning message itself quotes resource
names with Go `%q` double-quote formatting, which differs from the
remediation's shell-literal quoting.

| Command | Behavior |
|---|---|
| `install` | Prints warnings only for the resources the install selected, including members of a collection it installs. |
| `sync` | Prints grouped warnings. |
| `sync --check` | Prints grouped warnings. |
| `status` | Prints an `UNMET REQUIREMENTS` table with `RESOURCE`, `REQUIRES` (`<kind>:<name>`), and `FIX` columns, sorted by `RESOURCE` then `REQUIRES`. |
| `doctor` | Prints grouped warnings. |

## Exit status

`install`, `sync`, `sync --check`, `status`, and `doctor` exit with status 0
when unmet prerequisites are the only findings. `sync --strict` (including
`sync --strict --check`) still prints the advisory warning and remediation,
but exits nonzero so CI can enforce declared prerequisites. Strict mode never
selects the prerequisite for you.

## Related documentation

- [How to declare resource prerequisites](../how-to/author/declare-resource-prerequisites.md)
- [Resource metadata](./resource-metadata.md)
- [Registry layout](./registry-layout.md)
