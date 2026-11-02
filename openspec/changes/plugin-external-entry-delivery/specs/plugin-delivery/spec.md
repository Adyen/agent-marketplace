## ADDED Requirements

### Requirement: External catalog entries deliver from their locked commit

A plugin selected from a native plugin catalog entry whose source is an
external Git repository or Git subdirectory SHALL be delivered from the
package at the exact commit its acquisition lock records for that entry,
under the same delivery resolution, namespace, package generation, and
instance acceptance rules as any other plugin. Its delivered members SHALL
be the post-exclusion member keys committed in that lock record. Sync SHALL
NOT read the entry's live ref to deliver it. When the locked commit cannot
be opened or the entry has no locked record, that plugin instance SHALL be
blocked with an error naming the plugin, keeping its previous projection,
while other plugins and resources continue.

#### Scenario: External catalog entry is decomposed from its locked commit

- **WHEN** a `claude-marketplace` source lists an entry whose source is an
  external Git repository, the plugin is selected with delivery `auto` or
  `decompose`, and sync runs for a harness target without verified native
  delivery
- **THEN** each delivered member MUST be installed under `<plugin>-<member>`
  from the package at the commit the lock records for that repository, its
  plugin-root references MUST resolve into the instance's package
  generation, and no `plugin.delivery.pending` warning MUST be reported for
  the plugin

#### Scenario: External Git subdirectory entry delivers from that subdirectory

- **WHEN** a selected catalog entry's source is a subdirectory of an
  external Git repository
- **THEN** the delivered members MUST come from that subdirectory at the
  locked commit

#### Scenario: Moved external ref keeps delivering the locked commit

- **WHEN** the external repository's ref advances after the lock was written
  and `sync --locked` or `sync` runs without updating the lock
- **THEN** the delivered members MUST still come from the recorded commit

#### Scenario: External entry exclusions are honoured

- **WHEN** a plugin request for an external catalog entry excludes one of
  its members
- **THEN** the excluded member MUST NOT be installed, and the installed
  members MUST be exactly the post-exclusion member keys committed in the
  entry's lock record

#### Scenario: Delivered external entry reports no drift

- **WHEN** `sync --check` runs after a sync that delivered an external
  catalog entry
- **THEN** it MUST report no drift for that plugin and write nothing

#### Scenario: Unavailable locked external commit blocks only that plugin

- **WHEN** a selected external catalog entry has no locked record, or its
  locked commit cannot be opened in the current mode
- **THEN** that plugin instance MUST fail with an error naming the plugin,
  its previously installed members MUST stay unchanged, a first install
  MUST install none of its members, other plugins and resources MUST still
  be delivered, and the entry's live ref MUST NOT be acquired in its place
