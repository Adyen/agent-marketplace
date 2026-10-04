## ADDED Requirements

### Requirement: Retained safety payload reads retain admitted generation authority

Agent Marketplace SafetyBackups MUST admit payload shape and opened regular/single-link/nonblocking state beneath captured safety namespace without reading payload bytes during listing. Returned backups MUST privately retain original scope/name, parent-object and full payload identity; RawBytes MUST use that admitted authority rather than reopen mutable public path claims, compare observed states, read at captured size and revalidate named parent before returning bytes. Unsupported aliases/changed generations/links/special files and cleanup failures MUST return no payload. Unadmitted fabricated records MUST NOT grant read authority. Preserve faithful prior bytes/public path/manifest/JSON and allow harmless directory timestamp changes while checking device/inode. Publication/retention/evidence-action leases/namespaces remain separate; no new budget, durability, transaction or uncooperative-writer exclusion.

#### Scenario: Retained safety payload alias refuses before reading

- **WHEN** an actual listed retained backup payload is moved and replaced with an external alias
- **THEN** RawBytes MUST return typed filesystem refusal/no bytes and preserve original/foreign payloads, while restoring and relisting the regular backup MUST return exact actual prior bytes
