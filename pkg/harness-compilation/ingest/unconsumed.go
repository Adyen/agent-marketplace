package ingest

import (
	"fmt"
	"io/fs"
	"log/slog"
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

type configLocation struct {
	path string
	hint string
}

var knownConfigLocations = []configLocation{
	{path: ".mcp.json", hint: "Use a Claude plugin or bare-plugin layout, or move the MCP definitions into the selected layout's MCP configuration without overwriting existing entries."},
	{path: "mcp.json", hint: "For Claude plugins, rename to .mcp.json; alternatively add .factory-plugin/plugin.json or .cursor-plugin/plugin.json. Merge with any existing destination rather than overwriting it."},
	{path: "hooks/hooks.json", hint: "Use a Claude, Factory, or Cursor plugin layout, or move these hooks to the selected workspace's native hook configuration."},
	{path: ".claude/.mcp.json", hint: "Select a Claude workspace source and merge these MCP definitions into its selected configuration (.mcp.json takes precedence over .claude.json and .claude/.mcp.json)."},
	{path: ".claude.json", hint: "Select a Claude workspace source and merge these MCP definitions into its selected configuration (.mcp.json takes precedence over .claude.json and .claude/.mcp.json)."},
	{path: ".claude/hooks/hooks.json", hint: "Select a Claude workspace source, or move these hooks to hooks/hooks.json for a Claude plugin."},
	{path: ".claude/settings.json", hint: "Select a Claude workspace source; only the hooks member of this file is ingested."},
	{path: ".factory/hooks.json", hint: "Select a Factory workspace source to ingest the hooks in this standalone native file."},
	{path: ".cursor/hooks.json", hint: "Select a Cursor workspace source to ingest the hooks in this versioned native file."},
	{path: ".devin/hooks.json", hint: "Select a Windsurf source to ingest these Devin Desktop workspace hooks when no Devin CLI markers are present."},
	{path: ".factory/mcp.json", hint: "Select a Factory workspace source, or merge these MCP definitions into mcp.json for a Factory plugin."},
	{path: ".factory/hooks/hooks.json", hint: "Select a Factory workspace source, or merge these hooks into hooks/hooks.json for a Factory plugin."},
	{path: ".cursor/mcp.json", hint: "Select a Cursor workspace source, or merge these MCP definitions into mcp.json for a Cursor plugin."},
	{path: ".cursor/hooks/hooks.json", hint: "Select a Cursor workspace source, or merge these hooks into hooks/hooks.json for a Cursor plugin."},
	{path: "gemini-extension.json", hint: "Select a Gemini plugin source to ingest the MCP definitions in this extension manifest, or merge them into the selected layout's MCP configuration without overwriting existing entries."},
	{path: ".gemini/mcp.json", hint: "Select a Gemini source and merge the relevant definitions into .gemini/settings.json using Gemini's native schema."},
	{path: ".gemini/settings.json", hint: "Select a Gemini source and merge the relevant definitions into .gemini/settings.json using Gemini's native schema."},
	{path: ".gemini/gemini-extension.json", hint: "Select a Gemini source and merge the relevant definitions into .gemini/settings.json using Gemini's native schema."},
	{path: "mcp_config.json", hint: "Select the matching Windsurf or Devin source layout for this MCP configuration."},
	{path: ".windsurf/mcp_config.json", hint: "Select the matching Windsurf or Devin source layout for this MCP configuration."},
	{path: "hooks.json", hint: "Select a Windsurf source with this file at its native workspace root."},
	{path: ".windsurf/hooks.json", hint: "Select a Windsurf source with this file at its native workspace root."},
	{path: "hooks.v1.json", hint: "Select the matching Devin workspace or explicit direct-root source layout."},
	{path: ".devin/hooks.v1.json", hint: "Select the matching Devin workspace or explicit direct-root source layout."},
	{path: ".devin/config.json", hint: "Select the matching Devin workspace or explicit direct-root source layout."},
	{path: ".devin/mcp_config.json", hint: "Select the matching Devin workspace or explicit direct-root source layout."},
	{path: ".codex/hooks.json", hint: "Select a Codex workspace source to ingest this native configuration."},
	{path: ".codex/config.toml", hint: "Select a Codex workspace source to ingest this native configuration."},
}

func UnconsumedConfigs(src fs.FS, code, layout string, consumed ...string) []diag.Diagnostic {
	var diagnostics []diag.Diagnostic
	for _, candidate := range knownConfigLocations {
		if slices.Contains(consumed, candidate.path) {
			continue
		}
		info, err := fs.Stat(src, candidate.path)
		if err != nil || info.IsDir() {
			continue
		}
		message := fmt.Sprintf("%s found at bundle root but not ingested: bundle detected as %s layout. %s", candidate.path, layout, candidate.hint)
		diagnostics = append(diagnostics, diag.Diagnostic{Code: code, Severity: diag.SeverityWarning, Message: message, Hint: candidate.hint, Source: diag.SourceRef{File: candidate.path}})
		slog.Debug("unconsumed bundle configuration", "code", code, "layout", layout, "file", candidate.path)
	}
	return diagnostics
}
