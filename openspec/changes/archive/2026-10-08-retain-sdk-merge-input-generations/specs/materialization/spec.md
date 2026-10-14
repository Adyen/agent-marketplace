## ADDED Requirements

### Requirement: Marker merge publication retains its input generation

SDK marker-based aggregate merging MUST read existing payloads through captured regular singly linked nonblocking file authority, with captured-size and opened/named generation validation. The original input state or genuine absence MUST remain the authority for later comparison and publication. A newer destination MUST NOT authorize stale merged bytes, even when replacement bytes are identical; observed changes MUST refuse with the existing generation-conflict identity before staging or publication. Native consumers MUST expose their established coded refusal and preserve concurrent and retained original bytes, identity, permissions and modification time while cleaning only attempt-owned staging. Valid merges, shared permissions, comments, no-ops, private new creation, read-only planning and supported initial native aliases MUST remain usable. This is not an atomic hostile-writer exclusion, new processing ceiling, automatic retry or whole-operation transaction.

#### Scenario: Native marker merge preserves a concurrent input generation

- **WHEN** actual native MCP synchronization replaces the aggregate after its merge input is captured but before artifact comparison
- **THEN** it MUST refuse with `materialize.write.error`, preserve the concurrent and retained original generations without attempt staging, and a healthy retry MUST actually merge desired entries while preserving concurrent handwritten content

#### Scenario: SDK merge comparison retains original state and absence

- **WHEN** public SDK marker merging observes an input generation or absence and that state changes before comparison in a mutating or read-only operation
- **THEN** it MUST refuse with generation-conflict identity without consuming or changing the new generation or producing staging, while valid existing and absent input controls MUST still perform the intended merge or nonmutating plan
