## ADDED Requirements

### Requirement: Native delivery status may be declared per scope

A harness catalog SHALL be able to declare native delivery status separately
for user scope and repository scope. A catalog that declares one status
SHALL apply it to both scopes. Each scope declared verified SHALL carry the
identifier of a native probe present in the harness validation suite. The
generated plugin support reference SHALL show the native status per scope
when the scopes differ.

#### Scenario: A scope declared verified without a probe fails the check

- **WHEN** a harness catalog declares native delivery verified for one scope
  with no probe identifier, or with an identifier the harness validation
  suite does not define
- **THEN** the catalog check MUST fail and name the file and field

#### Scenario: The reference shows differing scope statuses

- **WHEN** a harness catalog declares native delivery verified at user scope
  and unverified at repository scope
- **THEN** the generated plugin support reference MUST show both statuses for
  that harness target
