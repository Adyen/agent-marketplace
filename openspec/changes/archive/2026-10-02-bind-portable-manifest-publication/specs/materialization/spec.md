## ADDED Requirements

### Requirement: Portable manifest mutation retains loaded namespace authority

Agent Marketplace MUST bind portable ownership-manifest publication/removal to the admitted scope, parent and loaded file generation. Existing scope/parent objects observed during loading MUST still match before staging or removal. The manifest MUST remain a direct regular singly-linked object with unchanged full identity and bytes, or remain absent when publication loaded absence. Publication MUST stage and rename through captured directories, and removal MUST unlink only the loaded unchanged manifest. Observed parent/leaf drift after staging MUST refuse with materialize.manifest.error, preserve replacement/external bytes, identity, permissions, modification time and aliases, and remove only attempt-owned staging in the captured directory. This is not a whole-installation transaction or exclusion of uncooperative writers between check and mutation, and adds no absolute byte or work threshold.

#### Scenario: Portable manifest removal preserves a replaced generation

- **WHEN** a valid native file-to-symlink migration replaces its loaded manifest generation before an empty-state removal
- **THEN** native migration may complete but manifest removal MUST report materialize.manifest.error and preserve the replacement manifest bytes/identity/mode/mtime

#### Scenario: Portable manifest publication preserves a replaced generation

- **WHEN** valid native migration leaves retained user-owned portable entries while the loaded manifest generation is replaced before publication
- **THEN** publication MUST refuse with materialize.manifest.error while preserving the replacement manifest and retained native user content

#### Scenario: Portable manifest staging keeps captured namespace authority

- **WHEN** a repository or machine manifest parent or leaf is replaced after the changed manifest is staged
- **THEN** metadata publication MUST refuse without mutating replacement/external content or aliases and MUST clean its own staged name from the captured directory

#### Scenario: Missing captured manifest parent stays absent on refusal

- **WHEN** a previously loaded manifest parent disappears before changed ownership metadata is published
- **THEN** publication MUST refuse with materialize.manifest.error without recreating the missing parent or changing the retained original manifest

### Requirement: Portable manifest updates retain faithful permissions and no-ops

Agent Marketplace MUST retain the existing admitted manifest permissions when publishing changed ownership state. Newly created manifests MUST retain 0644 permissions. Identical desired manifest bytes MUST preserve file identity and modification time after generation validation. Missing manifests remain valid, valid contained scope/parent aliases remain usable, and actual native ownership updates and valid empty-manifest removal MUST continue.

#### Scenario: Portable manifest repeats preserve identity and update permissions

- **WHEN** native repository or machine file-mode output repeats unchanged, updates content and later removes its owned selection
- **THEN** the manifest repeat MUST preserve identity/mode/mtime, the real ownership update MUST publish changed bytes with existing permissions, and empty-state removal MUST remove its unchanged manifest
