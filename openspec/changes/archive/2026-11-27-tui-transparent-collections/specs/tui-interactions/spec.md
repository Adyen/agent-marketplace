## ADDED Requirements

### Requirement: Individual collection members can be installed directly

Selecting an available member row beneath an expanded Git/local or Marketplace collection and pressing `space` SHALL allow installing that single member directly into a writable user, project, or local scope via the guarded install pipeline without installing the parent collection. Existing guards preventing partial removal of collection-only installed leaves SHALL remain unchanged.

#### Scenario: Installing a single member from an available collection

- **WHEN** a user expands an available collection, selects one MCP server member, and toggles it on at user or project scope
- **THEN** the TUI MUST install only that member as a direct selection in the chosen scope through the guarded mutation pipeline without installing the parent collection or sibling members
