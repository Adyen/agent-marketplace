## ADDED Requirements

### Requirement: Cache-backed link publication retains destination authority

Agent Marketplace MUST bind cache-backed symlink staging, publication and attempt-owned cleanup to the admitted destination directory. A parent or leaf whose observed generation changes before publication MUST be refused without replacing the new object or leaving attempt-owned staging in the captured or replacement tree. Refusal MUST report `materialize.write.error`. Existing user-owned collisions MUST remain preserved, and an unchanged managed link MUST remain a no-op. Existing file-mode output MAY migrate back to a cache-backed link only under its established ownership policy.

#### Scenario: Cache link publication refuses a replaced parent

- **WHEN** a cache-backed link's destination parent is replaced after staging and before publication
- **THEN** publication MUST refuse, preserve replacement content and remove only its own staged entry from the captured directory

#### Scenario: Cache link publication refuses a replaced leaf

- **WHEN** a cache-backed link's destination leaf is replaced after staging and before publication
- **THEN** publication MUST refuse and preserve the replacement leaf and the cache target

#### Scenario: Managed cache links update and repeat without mutation

- **WHEN** a managed cache link requires a new target and later repeats the same desired target
- **THEN** the update MUST publish at the native destination without changing cache payload bytes, and the repeat MUST preserve link identity and modification time
