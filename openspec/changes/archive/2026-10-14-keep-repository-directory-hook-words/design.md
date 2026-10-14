## Context

The hook catalog field `repository_base` tells the compiler how a repository-scope command reaches repository files. Windsurf declares `{cwd: repository}` because Cascade runs `command` via `bash -c` with `working_directory` defaulting to the workspace root. The rewrite removed the reference and one following `/`. Its only fallback for an empty result applied to the whole command, not to the shell word the reference occupied.

## Decisions

- **Whole word becomes `.`.** The repository directory relative to the hook working directory is `.`. The compiler emits it when the reference, optionally followed by one `/`, ends the shell word. An unquoted reference ends at the end of the command or at an unquoted space, tab, newline, `;`, `&`, `|`, `(`, `)`, `<`, or `>`. A double-quoted reference ends only at the closing quote, and the word must then end at one of those separators. Inside quotes the `.` stays quoted. A reference that is isolated in its own quotes and followed by `/` keeps the existing unquoted relative form.
- **Path components are unchanged.** `"$CLAUDE_PROJECT_DIR"/bin/check`, `"$CLAUDE_PROJECT_DIR/bin/check"`, and `$CLAUDE_PROJECT_DIR/bin/check` keep their current output, so installed hooks with a following path are not rewritten.
- **Joined text is refused.** Text joined to the reference without `/` cannot be expressed relative to the working directory. Suffixed words such as `${CLAUDE_PROJECT_DIR}-backup` or `"$CLAUDE_PROJECT_DIR is the root"` suppress the hook with `hook.env-unrepresentable` instead of silently changing the path. Prefixed words such as `--root=$CLAUDE_PROJECT_DIR` remain representable as `--root=.`.
- **Other repository bases are unaffected.** Variable and shell-expression repository bases substitute a value in place and are not changed.

## Risks / Trade-offs

- A Windsurf hook that used a suffix-joined project-directory reference was previously installed with a wrong path and is now refused. This fails closed and names the stable diagnostic.
