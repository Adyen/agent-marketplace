## ADDED Requirements

### Requirement: SDK file publication retains original destination state or absence

SDK Materialize MUST carry original opened artifact file identity/state and absence from comparison into publication. Full state or absence MUST remain unchanged before rename below captured output authority. Replacing an existing file or creating an originally absent leaf before publication MUST refuse generation conflict without changing replacement bytes/metadata or retained original and MUST clean only its attempt staging. Preserved merge modes MUST come from original opened file state. Existing healthy updates, source modes, no-op identity, staged-all-artifacts, dry/partial contracts remain compatible. This requirement does not retain original staged-file identity or fence stale discovery/removal by itself.

#### Scenario: SDK file publication preserves replaced destination generation

- **WHEN** public SDK Materialize replaces its admitted destination file or creates an originally absent destination after staging
- **THEN** publication MUST refuse without changing replacement bytes/inode/mode/mtime/member or retained original, clean its staging, and restoring original file or original absence MUST permit actual source-mode payload update
