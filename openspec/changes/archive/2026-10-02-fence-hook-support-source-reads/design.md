## Context

The hook support planner caches collection by source/target/harness, arbitrates same-output collisions and then appends support artifacts to compiled hooks. Path-based reads and silent skips can admit external payloads or remove support without blocking the hook.

## Decisions

1. Use captured directory enumeration and the same regular singly-linked source snapshots as the skill reader. Bytes/modes come from a stable admitted generation; captured size checks growth without a new absolute policy.
2. Cache both artifacts and refusal for each source context. Refuse the whole affected hook/source support set rather than emitting a partial prefix; independent sources keep existing arbitration.
3. Carry refusal to the existing blocked-resource and aggregate-incomplete machinery, with registered `materialize.hook.support-refused` severity error and value-free message/hint. Installed affected hook/support output remains retained.
4. Preserve existing primary/native sibling/Windsurf JSON exclusions, ordering, contained file aliases, modes and no-op behavior. Empty or absent optional source context is not promoted to cwd capture or a mandatory source.
5. Scope is support payload collection, not catalog/primary ingestion or later aggregate and ownership publication.

## Verification

The actual native Factory update witness failed on the parent by writing foreign support and changing hooks. Native installed preservation, contained alias/executable/no-op/source controls, valid siblings and built CLI v2/v4 checks pass. Gemini has no support-file output lane: its compatibility control proves unused source siblings do not block native settings updates or become output, while handwritten settings and permissions remain retained; no Gemini import closure is claimed. Linux/macOS, scoped validate/lint/build, count-20 security races, strict specs and the final combined E2E/configuration gate pass. The serialized 2s ×5 native repeat-sync disk/no-network benchmark reports 2.355ms → 3.295ms (+39.90%), 287329 → 293161 B/op (+2.03%) and 3088 → 3166 allocs/op (+2.53%), p=0.008 with host variance and no five-sample 95% interval. All 15 implementation entries, including exclusions, were adversarially reviewed; publication also reviews the normative/archive metadata.
