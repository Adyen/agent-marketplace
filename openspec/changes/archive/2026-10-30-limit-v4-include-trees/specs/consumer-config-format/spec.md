## ADDED Requirements

### Requirement: Include trees stay within the include limit

Loading one consumer root MUST refuse when it would read more than 256 config files. The count includes the root and every include occurrence on every branch, so a fragment reached through several branches counts once per branch. Refusal MUST report `v4.include-limit` before any acquisition, lock, or harness work. Include precedence, ordering, multiplicity and branch-local cycle detection MUST be unchanged for trees within the limit. The limit has no override.

#### Scenario: Include trees stay within the include limit

- **WHEN** a v4 root's includes form a shared diamond that expands to more than 256 files, or a flat list of 256 includes
- **THEN** the load MUST fail with `v4.include-limit`, and a root with 255 includes MUST still load
