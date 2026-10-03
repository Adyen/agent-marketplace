## ADDED Requirements

### Requirement: Native ownership marker payloads validate opened-file authority

Agent Marketplace native leading-frontmatter ownership marker checks MUST read only through captured regular, singly linked and nonblocking opened-file authority, bounded by captured size with read-state/named-target validation. Final symbolic marker leaves MUST NOT be followed. Unsafe or unreadable payloads and cleanup failures MUST NOT grant ownership; existing bool/no-ownership and coded user-owned collision preservation semantics MUST remain supported. Valid native markers MUST retain leading-frontmatter interpretation and payload/mode/no-op semantics without new arbitrary size limits. Later publication/prune generations remain separate contracts.

#### Scenario: Native ownership marker refusal preserves output

- **WHEN** native command update encounters a multiply linked or FIFO marker payload after a valid native marker installation
- **THEN** ownership checks MUST complete without accepting the claim or blocking and preserve native/retained bytes through the existing coded collision contract, while valid markers continue working
