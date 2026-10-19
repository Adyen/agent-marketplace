## ADDED Requirements

### Requirement: Verified archive decoding stops at the archive limits

Verifying a resource archive by digest, verifying a final archive against its verified placeholder, and decoding an archive for its content SHALL enforce the archive limits while the archive is decoded, not only after decoding completes. A member whose declared size exceeds its per-member limit, a member that would raise the asset, support or total file count or the total decoded bytes beyond the limits, more archive entries than the limits' files plus the canonical manifest and directories, and a repeated manifest SHALL be refused before that member's body is read and before any later entry is decoded. The manifest and members outside the canonical layout directories SHALL count toward the total decoded bytes. Archives within the limits SHALL verify as before.

#### Scenario: Verified archive decoding stops at the archive limits

- **WHEN** an archive verified by digest or decoded for content exceeds the total decoded bytes, the asset, support or total file count, the per-member size, or the entry count, or repeats its manifest, and is followed by an entry that decoding would otherwise reject
- **THEN** verification MUST fail with the limit that was exceeded
- **AND** the later entry MUST NOT be decoded
- **AND** an encoded archive with the maximum asset count and a member at the per-member limit MUST still verify
