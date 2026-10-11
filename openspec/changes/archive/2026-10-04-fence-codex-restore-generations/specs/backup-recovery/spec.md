## ADDED Requirements

### Requirement: Codex restore retains selected set generations through extraction

Agent Marketplace Codex set restore MUST retain captured set-parent and source archive generations across selection, preflight and extraction. Handles MUST remain bounded by declared native partition count and close on all returns. Before every partition mutation, original authorizing manifest full state, named parent, all source generations and canonical output-root keys MUST remain valid. Replacement members, even byte-identical, MUST refuse typed generation before they authorize writes. Preflight/extraction MUST reuse captured regular/single-link compressed-size reader authority without public archive-path reopening. Initial aliases, genuine missing-backup behavior and late partial-completion semantics remain; no new budget, whole-set restore transaction or uncooperative-writer exclusion.

#### Scenario: Codex restore refuses replaced preflight generation

- **WHEN** public CreateCodexSet archives real inert native bytes and public RestoreCodexSet encounters an identical-byte/mode archive or manifest replacement after preflight
- **THEN** it MUST refuse before native mutation, preserve current native inode/mode/mtime/bytes and replacement member bytes, and restoring the original member MUST allow genuine two-partition extraction of archived content
