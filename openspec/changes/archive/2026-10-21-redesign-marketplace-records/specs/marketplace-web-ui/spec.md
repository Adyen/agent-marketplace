## ADDED Requirements

### Requirement: Public Marketplace record visual hierarchy
Public resource detail, exact revision, registry history, and public state
surfaces SHALL use the shared typography, spacing, focus, geometry, motion,
maximum width, and outer gutters. Record presentation SHALL remain flat and
information-dense while technical values and status meaning remain visually
distinct.

#### Scenario: Public routes use shared foundations without changing behavior
- **WHEN** a user visits a public Marketplace record or state route
- **THEN** the route uses the shared visual foundations and aligned content edge
- **AND** resource identity, status, provenance, and technical values remain readable
- **AND** existing controls, destinations, responsive behavior, and safe rendering remain available

### Requirement: Marketplace records preserve technical context
Resource and registry record surfaces SHALL preserve identity, source,
revision, installation, content, security, registry, and technical information
through a consistent public hierarchy. Loading, unavailable, empty, and error
states SHALL retain their status text and recovery actions.

#### Scenario: Record hierarchy preserves inspection and recovery
- **WHEN** a user views a loaded or non-success resource or registry route
- **THEN** applicable identity, status, technical context, and recovery actions remain available
- **AND** copy, tabs, installation, and long-content containment retain their behavior
