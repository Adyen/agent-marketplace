## ADDED Requirements

### Requirement: Restart recovery actions retain original metadata authority

Agent Marketplace recovery MUST retain the originally opened journal and ownership-record identity/absence through phase decisions. Before recovery actions and within roll-forward state publication, original full metadata file state and captured parent object MUST remain valid. Identical JSON at a replacement inode MUST NOT authorize stale state overwrite or staged cleanup. Refusal MUST preserve replacement/original metadata and native/staged bytes; original restoration MUST permit real recovery. Existing missing/ordinary malformed-state behavior and JSON stay compatible. The existing optional Crasher MAY expose the read-to-action boundary; production installs none. No new budget, whole recovery transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Recovery refuses original journal replacement before action

- **WHEN** actual public Apply crashes with a prepared or aggregate-renamed journal and public Recover observes its metadata, then original journal or state is replaced with identical JSON
- **THEN** Recover MUST refuse before stale state publication or staged cleanup, preserve original/replacement/native bytes and allow genuine roll-forward/rollback after original metadata is restored
