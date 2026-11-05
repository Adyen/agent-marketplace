## ADDED Requirements

### Requirement: Ref bumping leaves exact commit pins unchanged

`update --bump` SHALL treat a source ref that is a full Git object ID, 40 hexadecimal characters or 64 in a SHA-256 repository in either letter case, as an exact commit pin. It MUST NOT rewrite that ref, MUST NOT count it in `refs bumped`, and MUST keep the acquisition lock on that commit, whatever tags the registry publishes. Every other ref SHALL keep its existing bump behavior.

#### Scenario: Ref bumping leaves a full commit pin unchanged

- **WHEN** a repository configuration pins one Git source to a full commit ID that ends in decimal digits, the registry publishes a tag named after the commit ID's non-numeric prefix followed by a larger number, and another source of the same registry uses ref `v0` while tag `v1` exists
- **THEN** `update --bump` MUST rewrite `v0` to `v1` and report `refs bumped: 1`
- **AND** the commit pin's configuration line, its acquisition lock commit and its installed resource MUST be unchanged
