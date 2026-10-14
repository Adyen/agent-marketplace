## ADDED Requirements

### Requirement: Project-directory references keep the repository directory when hooks run from it

When a harness target's hook catalog declares that repository hooks run from the repository root, the compiler SHALL translate each project-directory reference in a cross-harness hook command by its shell word:

- A reference that forms the whole shell word, alone or followed by one `/`, SHALL become `.`. This covers the bare `$NAME`, braced `${NAME}`, and double-quoted forms. A double-quoted reference SHALL keep its quotes.
- A reference followed by `/` and a further path component SHALL become that repository-relative path.
- A reference joined to following text without `/` has no repository-relative form. The hook SHALL be suppressed with the error `hook.env-unrepresentable` and no artifact, and the diagnostic SHALL NOT quote the command.

The generated command SHALL never contain an empty shell word in place of the repository directory.

#### Scenario: Whole-word project-directory references keep the repository directory

- **WHEN** Claude Code hooks whose commands are `cd $CLAUDE_PROJECT_DIR && pwd -P`, `cd ${CLAUDE_PROJECT_DIR} && pwd -P`, `cd "$CLAUDE_PROJECT_DIR" && pwd -P`, and `cd "$CLAUDE_PROJECT_DIR"/ && pwd -P` are compiled for Windsurf
- **THEN** the commands MUST be `cd . && pwd -P`, `cd . && pwd -P`, `cd "." && pwd -P`, and `cd . && pwd -P`, respectively
- **AND** a command of `"$CLAUDE_PROJECT_DIR"/bin/check` MUST still be `bin/check`
- **AND** when each installed handler is run from its native working directory at repository and user scope in both materialization modes, it MUST print the repository directory, never HOME

#### Scenario: Joined project-directory references refuse the hook

- **WHEN** a Claude Code hook whose command is `cp notes ${CLAUDE_PROJECT_DIR}-backup` is compiled for Windsurf
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error `hook.env-unrepresentable` without quoting the command
- **AND** the same hook compiled for Factory Droid MUST still use `FACTORY_PROJECT_DIR`
