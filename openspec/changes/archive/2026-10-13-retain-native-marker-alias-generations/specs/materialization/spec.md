## ADDED Requirements

### Requirement: Native marker alias detachment retains captured generation

Native marker aggregate alias detachment MUST retain the admitted parent identity, logical alias and target generation that supplied its payload until replacement or removal. Observed named-parent replacement, alias or target generation changes MUST refuse with the existing generation-conflict identity and native `materialize.write.error` diagnostic, including same-text alias replacement, byte-identical target replacement, retargeted source-directory aliases and empty captured targets. Read-only operations MUST refuse observed drift without mutation. Replacement staging, logical publication and attempt-owned cleanup MUST remain bound to the captured parent; foreign staged replacements MUST NOT be published or deleted. Supported initial aliases, original target preservation, handwritten native merges, existing shared-file permissions, empty-target private creation and repeat no-ops MUST remain compatible. This does not introduce atomic hostile-writer exclusion after the last check, automatic retry, a new ownership policy, processing ceilings or whole-operation rollback.

#### Scenario: Native marker alias detachment preserves concurrent generations

- **WHEN** an admitted native marker aggregate alias or its effective target changes, or its named parent is replaced, after payload capture or replacement staging in a mutating or read-only operation
- **THEN** synchronization MUST refuse with its existing generation-conflict identity and `materialize.write.error`, preserve concurrent and retained original bytes, identity, permissions and modification time without attempt-owned staging, and restoring valid authority MUST permit actual native merging

#### Scenario: Native marker alias staging preserves foreign replacements

- **WHEN** an admitted native marker aggregate alias has its prepared replacement stage replaced by an independently authored file before publication
- **THEN** synchronization MUST refuse, preserve the original alias and target and the foreign stage, publish no foreign stage bytes, and valid retry MUST still perform the desired native merge

#### Scenario: Empty native marker aliases retain private creation and no-ops

- **WHEN** an unchanged admitted native marker aggregate alias points to an empty regular target and synchronization installs a selected MCP server
- **THEN** synchronization MUST create private regular native output containing the selected server without changing the original target, and repeat synchronization MUST preserve output bytes, identity and modification time
