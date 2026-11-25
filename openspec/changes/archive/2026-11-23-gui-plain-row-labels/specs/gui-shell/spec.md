## ADDED Requirements

### Requirement: Resource rows show plain kind, scope, and harness labels

A resource row SHALL show the kind as its singular display name: Skill, Command, Subagent, Hook, MCP server, Collection, or Rule. A row whose scope is organization, project, or local SHALL show the scope tag Org, Project, or Local. A row with a registry-level harness override SHALL show an outlined tag reading "Only" followed by its harness names. Facet and section titles SHALL keep their plural names.

#### Scenario: Resource rows show plain kind, scope, and harness labels

- **WHEN** a resource row has the MCP server kind, organization scope, and a harness override for two harnesses
- **THEN** the row MUST show "MCP server" as its kind, "Org" as its scope tag, and an outlined "Only" tag listing the two harnesses
