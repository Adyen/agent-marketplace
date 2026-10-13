## ADDED Requirements

### Requirement: Cache namespace preparation retains admitted parent authority

Agent Marketplace MUST retain the selected cache parent directory during producer and compiled-directory namespace preparation. Creation MUST remain descriptor-relative and MUST refuse a named parent generation change before mutation. Private producer directories MUST start with mode0700 and their existing cleanup MUST retain admitted inode authority. Refusal MUST preserve foreign members/bytes/metadata and report materialize.write.error without installing the refused native link. Existing selected initial parent aliases remain compatible. This requirement does not make compilation/support/native publication a whole-tree transaction.

#### Scenario: Cache namespace creation refuses retargeted parent

- **WHEN** public native sync retargets the admitted cache parent before producer directory creation
- **THEN** namespace preparation MUST refuse without changing foreign members/bytes/inode/mode/mtime or leaving attempt members, keep native absent, and selecting an initial alias to the original parent MUST permit real source-mode native installation and own-staging cleanup
