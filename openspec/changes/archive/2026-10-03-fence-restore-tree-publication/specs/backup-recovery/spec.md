## ADDED Requirements

### Requirement: Restore tree namespace mutation stays under captured roots

Agent Marketplace directory and symbolic-leaf extraction MUST verify the named root generation against its captured authority before mutation. Directory preparation and symbolic-leaf parent traversal MUST stay below strict captured descriptors; symbolic namespace removal/link creation MUST NOT reopen an absolute output pathname. Existing entry path and symbolic-target policies MUST remain supported. This namespace control does not claim generation-atomic recursive member deletion, atomic symbolic replacement, all-output transactional extraction or exclusion of uncooperative writers, and adds no processing budget or durability sync.

#### Scenario: Restore tree retarget refuses before foreign mutation

- **WHEN** the extraction root is retargeted while compressed input is consumed after root capture for a directory or symbolic entry
- **THEN** extraction MUST refuse with typed generation identity before creating foreign entries, while valid input MUST really restore the selected directory or symbolic leaf
