## Why

Windsurf hooks run from the repository root, so its hook catalog declares the repository base `{cwd: repository}`: a project-directory reference is removed and the rest of the path becomes repository-relative. When the reference is the whole shell word, removing it leaves an empty word. A Claude Code hook `cd $CLAUDE_PROJECT_DIR && pwd -P` is installed as `cd  && pwd -P`, and runs in HOME instead of the repository (#648). A reference joined to other text, such as `${CLAUDE_PROJECT_DIR}-backup`, is installed as a different relative path.

## What Changes

- A project-directory reference that forms the whole shell word becomes `.` when the target's repository base is the hook working directory. Bare, braced, and double-quoted forms are covered, with or without a trailing `/`.
- A reference followed by `/` and a further path component keeps the existing repository-relative path.
- A reference joined to other text has no repository-relative form. The hook is suppressed with the error `hook.env-unrepresentable`, and the diagnostic does not quote the command.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-resources`: project-directory references keep the repository directory when the repository base is the hook working directory.

## Impact

Hook command rewriting in `pkg/harness-compilation/hookpolicy/resolve.go`, which only affects Windsurf because only its catalog declares `{cwd: repository}`. Compiler regression tests are added, and an E2E test executes the installed Windsurf handlers at repository and user scope in both materialization modes. Targets with a variable or shell-expression repository base are unchanged. Existing Windsurf hooks with a whole-word reference are rewritten on the next sync. Hooks with a joined reference are suppressed and keep their previously installed entries, as for other compile errors.
