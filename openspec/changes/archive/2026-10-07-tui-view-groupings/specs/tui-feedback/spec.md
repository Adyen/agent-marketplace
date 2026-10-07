## ADDED Requirements

### Requirement: Discoverable grouping and section toggle actions

Help SHALL advertise `v` for View settings and Space for resource or section toggles while retaining Enter details/fold and other folding controls. The palette SHALL offer View settings and Toggle all in section according to current list context and re-evaluate their guards on submission. Former tab switching SHALL remain unadvertised. Bulk feedback SHALL disclose matched, targeted, and skipped counts without claiming success before authoritative completion.

#### Scenario: Help and palette reflect section context

- **WHEN** the user inspects help and the palette on a resource or section, including an ineligible section or busy state
- **THEN** current grouping and section actions SHALL be discoverable in their eligible context, section submission SHALL use keyboard-path guards, former tab actions SHALL be absent, and counts SHALL reflect the confirmed target set

### Requirement: Privacy-safe bulk action telemetry

When TUI action telemetry is enabled, a submitted bulk toggle SHALL emit one action span with primary and secondary grouping axes, targeted and skipped counts, scope, and direction. It SHALL NOT contain resource names, source labels, filters, config paths, or registry coordinates, and SHALL retain the existing telemetry disclosure policy.

#### Scenario: Bulk action emits one bounded span

- **WHEN** a user submits a bulk toggle containing resources and labels with identifying text
- **THEN** one bulk action span SHALL record only grouping, counts, scope, and direction without that identifying text or obsolete tab attributes
