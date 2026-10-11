## ADDED Requirements

### Requirement: Sticky desktop filter rail remains reachable

The Marketplace browser SHALL keep every desktop filter control reachable once an over-height filter rail reaches its fixed sticky offset below the primary header.

#### Scenario: Over-height desktop filter rail scrolls

- **WHEN** the desktop filter rail contains more controls than fit below the primary header and reaches its sticky offset
- **THEN** the rail MUST remain bounded within the viewport and allow the user to scroll to its final control

## REMOVED Requirements

### Requirement: Desktop filter rail remains reachable

**Reason**: The fixed sticky primary header permits a simpler CSS-only rail whose height is bounded at its sticky offset; dynamic pre-sticky sizing is no longer required.

**Migration**: Use the fixed-offset sticky rail contract, which preserves independent scrolling and final-control reachability after the rail reaches its sticky position.
