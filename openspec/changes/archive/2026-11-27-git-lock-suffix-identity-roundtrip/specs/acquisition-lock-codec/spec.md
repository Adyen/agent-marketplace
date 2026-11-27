## ADDED Requirements

### Requirement: Repeated-suffix Git lock identities round-trip without collision

The v1 and v2 lock codec MUST preserve distinct canonical Git registry identities whose normalized repository path ends in `.git` across repeated decode, normalization, and encoding, using the existing URL field and version. It MUST validate each current URL value and leave ordinary single-suffix lock bytes unchanged.

#### Scenario: Repeated-suffix identities round-trip through v1 and v2

- **WHEN** v1 or v2 lock entries use repository URLs ending in `.git.git` and `.git.git.git` and are decoded, normalized, encoded, and decoded repeatedly
- **THEN** the canonical identities ending in `.git` and `.git.git` MUST remain distinct and the encoded URLs MUST retain their corresponding input spellings

#### Scenario: A changed normalized URL is validated independently

- **WHEN** a normalized repeated-suffix registry URL is changed before another normalization or encoding
- **THEN** the changed value MUST be validated as the current URL and MUST NOT inherit a stale suffix identity

#### Scenario: Ordinary single-suffix lock bytes remain stable

- **WHEN** an ordinary Git URL ending in one `.git` is encoded, including through a raw document literal
- **THEN** the canonical URL output MUST remain byte-identical to existing single-suffix lock goldens

### Requirement: Git lock lifecycle preserves repeated-suffix source identities

Lock writers and targeted upgrades MUST address the canonical identity produced by removing exactly one terminal `.git`. Targeted-upgrade selectors MUST compare canonical URLs directly without re-canonicalizing either value; they MUST NOT collapse that identity with a shorter repository or change another configured source.

#### Scenario: Targeted upgrade retains repeated and ordinary sources

- **WHEN** a v4 repository config contains a repeated-suffix Git source and a distinct ordinary Git source, the repeated source advances, and `lock --upgrade` targets only that source
- **THEN** the lock MUST record the repeated source's new commit with its distinct identity, retain the ordinary source unchanged, and allow `sync --locked` and `sync --check` to use the updated lock

#### Scenario: Missing configured fetch mapping retains the repeated-suffix origin

- **WHEN** a locked repeated-suffix Git registry has no configured fetch mapping
- **THEN** its fetch fallback MUST use the corresponding repeated-suffix origin rather than a shortened repository URL
