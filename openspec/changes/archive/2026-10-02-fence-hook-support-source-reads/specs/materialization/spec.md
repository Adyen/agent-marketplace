## ADDED Requirements

### Requirement: Hook support source refusal preserves native hook content

Agent Marketplace MUST enumerate hook support sources through captured directory handles and authorize regular singly-linked snapshots before reading payload. Byte content and permissions MUST come from the same admitted unchanged generation. Source read/admission failure MUST block affected hooks and support artifacts with registered severity-error diagnostic `materialize.hook.support-refused`, without echoing payload values or installing a partial source support set. Installed affected native content MUST be retained. Existing source/target exclusions, optional absent context, contained relative aliases and conflict arbitration MUST remain compatible. This introduces no new absolute source or aggregate-work threshold.

#### Scenario: Hook support source refusal preserves installed native content

- **WHEN** an installed native hook update has an external source support alias
- **THEN** actual native materialization MUST report source refusal, preserve installed hook/support and external bytes/identity/mode/mtime, and publish no foreign support

#### Scenario: Contained hook support alias keeps native content and mode

- **WHEN** a native hook source support alias points to a regular executable file inside its captured directory
- **THEN** native installation and repeat sync MUST retain hook/support bytes and executable mode, leave the source untouched and avoid rewriting unchanged support

#### Scenario: Hook source refusal keeps independent valid native work

- **WHEN** one selected hook support source refuses while a distinct source has a valid hook and support payload
- **THEN** materialization MUST block only the refused source's hooks/support and still emit the independent valid native hook/support

#### Scenario: Targets without hook support lanes do not read unneeded sources

- **WHEN** Gemini native hooks update settings while source siblings are not part of a support-file output lane
- **THEN** uncopied source siblings MUST NOT block the valid native settings update or become support artifacts; handwritten settings and native permissions remain preserved
