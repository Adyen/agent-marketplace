## Context

The hook support planner caches collection by source/target/harness, arbitrates same-output collisions and then appends support artifacts to compiled hooks. Path-based reads and silent skips can admit external payloads or remove support without blocking the hook.

## Decisions

1. Use captured directory enumeration and the same regular singly-linked source snapshots as the skill reader. Bytes/modes come from a stable admitted generation; captured size checks growth without a new absolute policy.
2. Cache both artifacts and refusal for each source context. Refuse the whole affected hook/source support set rather than emitting a partial prefix; independent sources keep existing arbitration.
3. Carry refusal to the existing blocked-resource and aggregate-incomplete machinery, with registered `materialize.hook.support-refused` severity error and value-free message/hint. Installed affected hook/support output remains retained.
4. Preserve existing primary/native sibling/Windsurf JSON exclusions, ordering, contained file aliases, modes and no-op behavior. Empty or absent optional source context is not promoted to cwd capture or a mandatory source.
5. Scope is support payload collection, not catalog/primary ingestion or later aggregate and ownership publication.

## Verification

The actual native Factory update witness failed on the parent by writing foreign support and changing hooks. Contained alias/executable/no-op/source controls, valid siblings and native durable-aggregate behavior must remain checked, with CLI, Linux/macOS, repeated races, specs and measured cost before publication.
