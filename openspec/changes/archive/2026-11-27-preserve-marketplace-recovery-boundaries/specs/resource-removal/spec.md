# Spec Delta

## MODIFIED Requirements

### Requirement: Marketplace selections are removable without a lock record

Named removal SHALL accept a Marketplace selection from a writable user,
repository, or personal-local layer when that layer has no acquisition lock or
its lock has no record for the selection. Removal SHALL edit only the
configuration, SHALL make no Marketplace request for the removed selection, and
SHALL then synchronize as usual. Unsupported or invalid configuration roots and
required selections SHALL keep their existing refusals.

An unrelated active native hook MUST NOT block removal of an unrecorded optional direct skill, command, subagent, or MCP server selection without an explicit revision. A hook or collection whose removal may affect active native hooks still requires evidence of the prior selection.

#### Scenario: Remove a Marketplace selection when the layer has no lock

- **GIVEN** a repository configuration selects two Marketplace resources, no repository acquisition lock exists, and one resource's latest-revision lookup returns 404
- **WHEN** the user runs `agent-marketplace remove <name> --scope repo` for that resource
- **THEN** removal MUST succeed and remove only that selection from the configuration
- **AND** the following `sync` MUST exit 0, write a lock recording the remaining resource, and install it

#### Scenario: Remove a Marketplace selection missing from an existing lock

- **GIVEN** a repository acquisition lock records the endpoint but not a selected resource
- **WHEN** the user removes that resource by name
- **THEN** removal MUST succeed and leave the lock's other records unchanged

#### Scenario: Remove an unrecorded non-hook selection with an unrelated active hook

- **WHEN** the user removes an optional direct Marketplace skill, command, subagent, or MCP server without an explicit revision or lock record while an unrelated handwritten native hook is active
- **THEN** removal MUST succeed without requesting the removed resource or changing the handwritten hook

#### Scenario: Missing hook evidence prevents removal with active native hooks

- **GIVEN** an installed Marketplace hook or collection containing a hook, an active native hook, and no acquisition lock or local ownership proof
- **WHEN** the user removes that selection by name
- **THEN** removal MUST refuse without changing the configuration or native hook file, creating the absent lock, or creating ownership state
