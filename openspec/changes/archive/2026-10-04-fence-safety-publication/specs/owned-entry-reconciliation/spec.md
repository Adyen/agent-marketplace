## ADDED Requirements

### Requirement: Safety publication uses captured private namespace and producer authority

Agent Marketplace Apply/BackupOnly MUST prepare ownership/safety namespaces through strict captured directory components with private creation and preserve existing directory modes. Safety payload creation/sync and sidecar staging/publication MUST use one captured safety parent, revalidate named parent and actual produced payload state before publishing claims, and remove only tracked attempt inodes on refusal. Preserve byte-exact prior payloads, private 0700 directories/0600 files, current filenames/JSON/crash behavior and no foreign writes or replacement deletion on refusal. Retention/CAS safety cleanup and prior record/action leases remain separate; no new budget, overall transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Safety publication refuses retargeted directory

- **WHEN** actual BackupOnly retargets its prepared safety directory to an external directory before payload creation
- **THEN** it MUST return typed filesystem refusal/no path with unchanged native/foreign bytes and no attempt residue, while restoring the directory MUST permit real readable byte-exact private safety publication
