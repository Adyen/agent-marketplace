package cursor

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	cursorPluginManifest = ".cursor-plugin/plugin.json"
	cursorWorkspaceRoot  = ".cursor/"
)

func init() {
	ingest.Register(&cursorIngester{})
}

type cursorIngester struct{}

type roots struct {
	skills   string
	commands string
	agents   string
	hooks    string
	mcp      string
	rules    string
	strip    string
}

func (c *cursorIngester) Source() ingest.Harness {
	return ingest.HarnessCursor
}

func (c *cursorIngester) Detect(src fs.FS) bool {
	if _, err := fs.ReadFile(src, cursorPluginManifest); err == nil {
		return true
	}
	if hasWorkspaceForm(src) {
		return true
	}
	if hasPluginResources(src) {
		return true
	}
	return false
}

func (c *cursorIngester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	switch {
	case hasPluginResources(src):
		return loadBundle(src, roots{
			skills:   "skills",
			commands: "commands",
			agents:   "agents",
			hooks:    "hooks/hooks.json",
			mcp:      "mcp.json",
			rules:    "rules",
		}, false)
	case hasWorkspaceForm(src):
		return loadBundle(src, roots{
			skills:   ".cursor/skills",
			commands: ".cursor/commands",
			agents:   ".cursor/agents",
			hooks:    ".cursor/hooks/hooks.json",
			mcp:      ".cursor/mcp.json",
			rules:    ".cursor/rules",
			strip:    cursorWorkspaceRoot,
		}, true)
	default:
		return nil, fmt.Errorf("ingest/cursor: no recognizable Cursor layout found")
	}
}

func LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	return (&cursorIngester{}).LoadBundle(src)
}

func LoadSkill(src fs.FS, path string) (*ir.Skill, error) {
	skill, err := bundle.LoadSkill(src, path)
	if err != nil {
		return nil, err
	}
	skill.Source.File = normalizeSourcePath(skill.Source.File)
	skill.ModelSource = "cursor"
	return skill, nil
}

func LoadCommand(src fs.FS, path string) (*ir.Command, error) {
	cmd, err := bundle.LoadCommand(src, path)
	if err != nil {
		return nil, err
	}
	cmd.Source.File = normalizeSourcePath(cmd.Source.File)
	return cmd, nil
}

// LoadRule reads a Cursor rule (.cursor/rules/<name>.mdc) via the shared bundle
// loader, which auto-detects the .mdc frontmatter (globs/alwaysApply/description).
func LoadRule(src fs.FS, path string) (*ir.Rule, error) {
	rule, err := bundle.LoadRule(src, path)
	if err != nil {
		return nil, err
	}
	rule.Source.File = normalizeSourcePath(rule.Source.File)
	return rule, nil
}

func LoadSubagent(src fs.FS, path string) (*ir.Subagent, error) {
	sub, err := bundle.LoadSubagent(src, path)
	if err != nil {
		return nil, err
	}
	sub.Source.File = normalizeSourcePath(sub.Source.File)
	sub.ModelSource = "cursor"
	for i, tool := range sub.Tools {
		if reversed, ok := mapping.ReverseTool(tool, mapping.TargetCursor); ok {
			sub.Tools[i] = reversed
		}
	}
	return sub, nil
}

func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []*diag.Diagnostic, error) {
	hooks, diags, err := bundle.LoadHooks(src, path)
	if err != nil {
		return nil, nil, err
	}
	for _, h := range hooks {
		h.Source.File = normalizeSourcePath(h.Source.File)
		h.Command = strings.ReplaceAll(h.Command, "${CURSOR_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}")
	}
	for _, d := range diags {
		d.Source.File = normalizeSourcePath(d.Source.File)
	}
	return hooks, diags, nil
}

func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error) {
	servers, err := bundle.LoadMcpServers(src, path)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		server.Source.File = normalizeSourcePath(server.Source.File)
	}
	return servers, nil
}

func loadBundle(src fs.FS, paths roots, synthesizedManifest bool) (*ingest.Bundle, error) {
	var resources []ir.Resource
	var diags []diag.Diagnostic

	// Registry-supplied manifest: duplicate keys are rejected before decoding
	// because name/version feed ownership markers and output paths.
	manifest := ir.Manifest{}
	if err := ingest.LoadJSONManifest(src, cursorPluginManifest, &manifest); err != nil && !isNotExist(err) {
		slog.Error("parse Cursor manifest failed", "path", cursorPluginManifest, "error", err)
		return nil, fmt.Errorf("ingest/cursor: parse %s: %w", cursorPluginManifest, err)
	}

	skillDirs, err := listSubdirs(src, paths.skills)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/cursor: walk skills: %w", err)
	}
	for _, dir := range skillDirs {
		skill, err := LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestCursorLoadSkillFailed, "skill", normalizeResourceName(dir), dir, err))
			continue
		}
		resources = append(resources, skill)
	}

	cmdFiles, err := listMDFiles(src, paths.commands)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/cursor: walk commands: %w", err)
	}
	for _, file := range cmdFiles {
		cmd, err := LoadCommand(src, file)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestCursorLoadCommandFailed, "command", normalizeResourceName(file), file, err))
			continue
		}
		resources = append(resources, cmd)
	}

	agentFiles, err := listMDFiles(src, paths.agents)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/cursor: walk agents: %w", err)
	}
	for _, file := range agentFiles {
		sub, err := LoadSubagent(src, file)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestCursorLoadSubagentFailed, "subagent", normalizeResourceName(file), file, err))
			continue
		}
		resources = append(resources, sub)
	}

	hooks, hookDiags, err := LoadHooks(src, paths.hooks)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/cursor: load hooks: %w", err)
	}
	if err == nil {
		for _, d := range hookDiags {
			diags = append(diags, *d)
		}
		for _, h := range hooks {
			resources = append(resources, h)
		}
	}

	servers, err := LoadMcpServers(src, paths.mcp)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/cursor: load mcp servers: %w", err)
	}
	if err == nil {
		for _, server := range servers {
			resources = append(resources, server)
		}
	}

	if paths.rules != "" {
		ruleFiles, err := listRuleFiles(src, paths.rules)
		if err != nil && !isNotExist(err) {
			return nil, fmt.Errorf("ingest/cursor: walk rules: %w", err)
		}
		for _, file := range ruleFiles {
			rule, err := LoadRule(src, file)
			if err != nil {
				diags = append(diags, ingest.LoadFailure(diag.CodeIngestCursorLoadRuleFailed, "rule", normalizeResourceName(file), file, err))
				continue
			}
			resources = append(resources, rule)
		}
	}

	if synthesizedManifest {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestCursorManifestSynthesized,
			Severity: diag.SeverityInfo,
			Message:  "workspace form: manifest synthesized (no .cursor-plugin/plugin.json found)",
		})
	}

	layout := "cursor bare-plugin"
	if synthesizedManifest {
		layout = "cursor workspace"
	} else if _, err := fs.Stat(src, cursorPluginManifest); err == nil {
		layout = "cursor plugin"
	}
	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestCursorUnconsumedConfig, layout, paths.hooks, paths.mcp)...)
	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

func hasWorkspaceForm(src fs.FS) bool {
	checks := []string{".cursor/skills", ".cursor/commands", ".cursor/agents", ".cursor/hooks", ".cursor/rules"}
	for _, dir := range checks {
		if entries, err := fs.ReadDir(src, dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	if _, err := fs.ReadFile(src, ".cursor/mcp.json"); err == nil {
		return true
	}
	return false
}

func hasPluginResources(src fs.FS) bool {
	checks := []string{"skills", "commands", "agents", "hooks"}
	for _, dir := range checks {
		if entries, err := fs.ReadDir(src, dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	if _, err := fs.ReadFile(src, "mcp.json"); err == nil {
		return true
	}
	return false
}

// listRuleFiles lists Cursor rule files (.mdc, plus .md for tolerance) in root.
func listRuleFiles(src fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".mdc") || strings.HasSuffix(e.Name(), ".md") {
			files = append(files, root+"/"+e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func normalizeSourcePath(path string) string {
	return strings.TrimPrefix(path, cursorWorkspaceRoot)
}

func normalizeResourceName(path string) string {
	return strings.TrimPrefix(path, cursorWorkspaceRoot)
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func listSubdirs(src fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, root+"/"+e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func listMDFiles(src fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, root+"/"+e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}
