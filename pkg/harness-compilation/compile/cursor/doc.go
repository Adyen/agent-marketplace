// Package cursor implements the Cursor-target translator for the harness-compilation
// library. Cursor's layout is isomorphic to Factory with different directory names:
// skills/<name>/SKILL.md, commands/<name>.md, agents/<name>.md, hooks/hooks.json,
// mcp.json.
//
// Cursor also has a 'rules' (.mdc) primitive: ir.Rule resources compile to
// rules/<name>.mdc with YAML frontmatter (description, globs, alwaysApply).
package cursor
