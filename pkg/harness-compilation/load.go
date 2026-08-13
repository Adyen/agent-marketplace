package harnesscompile

import (
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Resource types re-exported from ir so a consumer that imports only this
// package can name every value the Load* functions return.
type (
	Skill     = ir.Skill
	Command   = ir.Command
	Subagent  = ir.Subagent
	Hook      = ir.Hook
	McpServer = ir.McpServer
	Rule      = ir.Rule
)

// LoadSkill reads a single skill directory from src at the given path. The
// directory must contain a SKILL.md file with YAML frontmatter; the skill name
// defaults to the directory name when frontmatter omits it.
//
// It is a thin facade over bundle.LoadSkill.
func LoadSkill(src fs.FS, path string) (*Skill, error) {
	skill, err := bundle.LoadSkill(src, path)
	if err != nil {
		slog.Error("load skill failed", "path", path, "error", err)
		return nil, fmt.Errorf("harnesscompile.LoadSkill %q: %w", path, err)
	}
	return skill, nil
}

// LoadCommand reads a single command .md file from src at the given path.
//
// It is a thin facade over bundle.LoadCommand.
func LoadCommand(src fs.FS, path string) (*Command, error) {
	command, err := bundle.LoadCommand(src, path)
	if err != nil {
		slog.Error("load command failed", "path", path, "error", err)
		return nil, fmt.Errorf("harnesscompile.LoadCommand %q: %w", path, err)
	}
	return command, nil
}

// LoadSubagent reads a single subagent .md file from src at the given path.
//
// It is a thin facade over bundle.LoadSubagent.
func LoadSubagent(src fs.FS, path string) (*Subagent, error) {
	subagent, err := bundle.LoadSubagent(src, path)
	if err != nil {
		slog.Error("load subagent failed", "path", path, "error", err)
		return nil, fmt.Errorf("harnesscompile.LoadSubagent %q: %w", path, err)
	}
	return subagent, nil
}

// LoadHooks reads a hooks.json file from src at the given path and flattens it
// into one Hook per event+rule combination. Both the Claude Code workspace
// format (top-level event keys) and the IR format (nested under "hooks") are
// accepted.
//
// Loading hooks is lossy for hook types that are not universally supported
// (http, prompt, agent): those produce warning diagnostics instead of an
// error, so the diagnostics are returned to the caller rather than dropped.
//
// It is a thin facade over bundle.LoadHooks.
func LoadHooks(src fs.FS, path string) ([]*Hook, []Diagnostic, error) {
	hooks, diags, err := bundle.LoadHooks(src, path)
	if err != nil {
		slog.Error("load hooks failed", "path", path, "error", err)
		return nil, nil, fmt.Errorf("harnesscompile.LoadHooks %q: %w", path, err)
	}
	return hooks, flattenDiagnostics(diags), nil
}

// LoadMcpServers reads an MCP configuration file (for example .mcp.json) from
// src at the given path and returns one McpServer per configured entry.
//
// It is a thin facade over bundle.LoadMcpServers.
func LoadMcpServers(src fs.FS, path string) ([]*McpServer, error) {
	servers, err := bundle.LoadMcpServers(src, path)
	if err != nil {
		slog.Error("load mcp servers failed", "path", path, "error", err)
		return nil, fmt.Errorf("harnesscompile.LoadMcpServers %q: %w", path, err)
	}
	return servers, nil
}

// flattenDiagnostics converts the pointer diagnostics produced by the bundle
// loaders into the value diagnostics used everywhere else in the public API.
func flattenDiagnostics(diags []*diag.Diagnostic) []Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d == nil {
			continue
		}
		out = append(out, *d)
	}
	return out
}
