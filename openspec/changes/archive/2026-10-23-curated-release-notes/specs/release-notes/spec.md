## ADDED Requirements

### Requirement: Release notes from archived change proposals

`specaudit release-notes -base <ref> -head <ref>` SHALL render Markdown from the `## Release note` sections of archived change proposals that the range adds. A `Breaking:` note SHALL appear under `## Breaking changes` with the prefix removed, and every other qualifying note under `## Changes`. Each entry SHALL link the pull request that merged the proposal's commit when the history contains one. With `-o`, the command SHALL write the Markdown to the file, leaving it empty when nothing qualifies.

#### Scenario: Breaking notes are listed before changes

- **WHEN** a range adds archived proposals with a normal note and a `Breaking:` note
- **THEN** the output MUST list the breaking entry, without its prefix, under `## Breaking changes` before the `## Changes` section

#### Scenario: None and section-less proposals are omitted

- **WHEN** a range adds archived proposals whose note is `none — <reason>` or that have no release note section
- **THEN** the output MUST NOT contain entries for them

#### Scenario: Entries link the merging pull request

- **WHEN** an archived proposal's commit is an ancestor of the second parent of a commit in the range whose subject starts `Merge pull request #N from `, the form GitHub writes for merge commits, and not of its first parent
- **THEN** the first paragraph of its entry MUST end with a link to pull request N when a repository URL is available, or with `(#N)` when no repository URL can be resolved
- **AND** an entry whose commit was committed directly to the base branch and not merged in by a pull request MUST have no pull request reference

#### Scenario: Multi-line notes keep their structure

- **WHEN** a `Breaking:` note has a sentence followed by a numbered upgrade list
- **THEN** the entry MUST start with the sentence as its bullet text, ending its first paragraph with the pull request link
- **AND** every later line of the note MUST be indented under the bullet, with blank lines kept blank

#### Scenario: Nothing qualifies yields empty notes

- **WHEN** no archived proposal in the range has a qualifying note
- **THEN** the output file MUST be empty and the command MUST succeed
