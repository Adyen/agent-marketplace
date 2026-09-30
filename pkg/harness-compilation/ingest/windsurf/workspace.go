package windsurf

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type workspaceRoot struct {
	fs       fs.FS
	base     string
	isBundle bool
}

func LoadWorkspace(src fs.FS, opts LoadOptions) (*ingest.Bundle, error) {
	return loadWorkspace(src, opts, ".windsurf")
}

func loadWorkspace(src fs.FS, opts LoadOptions, sourceBase string) (*ingest.Bundle, error) {
	roots := make([]workspaceRoot, 0, 2)
	if opts.User != nil {
		roots = append(roots, workspaceRoot{fs: opts.User, base: "."})
	}
	roots = append(roots, workspaceRoot{fs: src, base: sourceBase, isBundle: true})

	skills := map[string]*ir.Skill{}
	commands := map[string]*ir.Command{}
	hooks := map[string]*ir.Hook{}
	servers := map[string]*ir.McpServer{}
	rules := map[string]*ir.Rule{}
	var diags []diag.Diagnostic
	var currentHooks []*ir.Hook

	for _, root := range roots {
		loadedSkills, err := loadSkills(root)
		if err != nil {
			return nil, err
		}
		for name, skill := range loadedSkills {
			skills[name] = skill
		}

		loadedRules, err := loadRules(root)
		if err != nil {
			return nil, err
		}
		for name, rule := range loadedRules {
			rules[name] = rule
		}

		loadedCommands, workflowDiags, err := loadWorkflows(root)
		if err != nil {
			return nil, err
		}
		diags = append(diags, workflowDiags...)
		for name, command := range loadedCommands {
			commands[name] = command
		}

		loadedHooks, hookDiags, hookPath, err := loadHooksRoot(root, opts)
		if err != nil {
			return nil, err
		}
		diags = append(diags, hookDiags...)
		if opts.LegacyHookInventory {
			for _, hook := range loadedHooks {
				hooks[hook.Name()] = hook
			}
		} else {
			currentHooks = append(currentHooks, loadedHooks...)
		}

		loadedServers, mcpDiags, mcpPath, err := loadMcpRoot(root)
		if err != nil {
			return nil, err
		}
		diags = append(diags, mcpDiags...)
		for name, server := range loadedServers {
			servers[name] = server
		}

		agentsPath := "AGENTS.md"
		if root.base == "." {
			agentsPath = "AGENTS.md"
		}
		if fileExists(root.fs, agentsPath) {
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeIngestWindsurfSubagentViaAgentsMDSkipped,
				Severity: diag.SeverityInfo,
				Message:  "AGENTS.md is handled by existing conventions; no Windsurf subagent resources were synthesized",
				Source:   diag.SourceRef{File: agentsPath, Line: 1},
			})
		}
		if !root.isBundle {
			continue
		}
		consumed := make([]string, 0, 2)
		if hookPath != "" {
			consumed = append(consumed, hookPath)
			if hookPath == "hooks" {
				consumed = append(consumed, "hooks/hooks.json")
			}
		}
		if mcpPath != "" {
			consumed = append(consumed, mcpPath)
		}
		layout := "windsurf direct-root"
		if root.base != "." {
			layout = "windsurf workspace"
		}
		diags = append(diags, ingest.UnconsumedConfigs(root.fs, diag.CodeIngestWindsurfUnconsumedConfig, layout, consumed...)...)
	}
	if opts.LegacyHookInventory {
		currentHooks = sortHooks(hooks)
	} else {
		distinctHookNames(currentHooks)
	}

	var resources []ir.Resource
	for _, skill := range sortSkills(skills) {
		resources = append(resources, skill)
	}
	for _, rule := range sortRules(rules) {
		resources = append(resources, rule)
	}
	for _, command := range sortCommands(commands) {
		resources = append(resources, command)
	}
	for _, hook := range currentHooks {
		resources = append(resources, hook)
	}
	for _, server := range sortServers(servers) {
		resources = append(resources, server)
	}

	diags = append([]diag.Diagnostic{{
		Code:     diag.CodeIngestWindsurfManifestSynthesized,
		Severity: diag.SeverityInfo,
		Message:  "Windsurf has no plugin manifest; manifest synthesized from workspace layout",
	}}, diags...)

	return &ingest.Bundle{Manifest: ir.Manifest{}, Resources: resources, Diagnostics: diags}, nil
}

func loadSkills(root workspaceRoot) (map[string]*ir.Skill, error) {
	dirs, err := listSubdirs(root.fs, path.Join(root.base, "skills"))
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/windsurf: walk skills: %w", err)
	}
	resources := map[string]*ir.Skill{}
	for _, dir := range dirs {
		skill, err := LoadSkill(root.fs, dir)
		if err != nil {
			return nil, err
		}
		resources[skill.Name()] = skill
	}
	return resources, nil
}

func loadRules(root workspaceRoot) (map[string]*ir.Rule, error) {
	files, err := listRuleFiles(root.fs, path.Join(root.base, "rules"))
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/windsurf: walk rules: %w", err)
	}
	resources := map[string]*ir.Rule{}
	for _, file := range files {
		rule, err := LoadRule(root.fs, file)
		if err != nil {
			return nil, err
		}
		resources[rule.Name()] = rule
	}
	return resources, nil
}

func loadWorkflows(root workspaceRoot) (map[string]*ir.Command, []diag.Diagnostic, error) {
	files, err := listMDFiles(root.fs, path.Join(root.base, "workflows"))
	if err != nil && !isNotExist(err) {
		return nil, nil, fmt.Errorf("ingest/windsurf: walk workflows: %w", err)
	}
	resources := map[string]*ir.Command{}
	var diags []diag.Diagnostic
	for _, file := range files {
		command, commandDiags, err := LoadWorkflow(root.fs, file)
		if err != nil {
			return nil, nil, err
		}
		resources[command.Name()] = command
		diags = append(diags, commandDiags...)
	}
	return resources, diags, nil
}

func loadHooksRoot(root workspaceRoot, opts LoadOptions) ([]*ir.Hook, []diag.Diagnostic, string, error) {
	var hooks []*ir.Hook
	var diags []diag.Diagnostic
	var err error
	selected := path.Join(root.base, "hooks.json")
	if opts.LegacyHookInventory {
		selected = path.Join(root.base, "hooks")
		hooks, diags, err = loadHooksWithOptions(root.fs, selected, opts)
		if errors.Is(err, fs.ErrNotExist) {
			selected = path.Join(root.base, "hooks.json")
			hooks, diags, err = loadHooksWithOptions(root.fs, selected, opts)
		}
	} else {
		hooks, diags, err = loadHooksWithOptions(root.fs, selected, opts)
		if errors.Is(err, fs.ErrNotExist) {
			selected = path.Join(root.base, "hooks")
			hooks, diags, err = loadHooksWithOptions(root.fs, selected, opts)
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, selected, err
	}
	if errors.Is(err, fs.ErrNotExist) {
		selected = ""
	}
	return hooks, diags, selected, nil
}

func loadMcpRoot(root workspaceRoot) (map[string]*ir.McpServer, []diag.Diagnostic, string, error) {
	paths := []string{path.Join(root.base, "mcp_config.json")}
	if root.base != "." {
		// Backward compatibility for older Windsurf layouts where
		// mcp_config.json may still exist at the workspace root.
		paths = append(paths, "mcp_config.json")
	}

	for _, file := range paths {
		servers, diags, err := LoadMcpServers(root.fs, file)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, nil, file, err
		}
		resources := map[string]*ir.McpServer{}
		for _, server := range servers {
			resources[server.Name()] = server
		}
		return resources, diags, file, nil
	}
	return map[string]*ir.McpServer{}, nil, "", nil
}

func hasWorkspaceLayout(src fs.FS, base string) bool {
	checks := []string{
		path.Join(base, "skills"),
		path.Join(base, "rules"),
		path.Join(base, "workflows"),
		path.Join(base, "hooks"),
		path.Join(base, "hooks.json"),
		path.Join(base, "mcp_config.json"),
	}
	if base == "." {
		checks = append(checks, "AGENTS.md")
	}
	for _, check := range checks {
		if fileExists(src, check) || dirHasEntries(src, check) {
			return true
		}
	}
	return false
}

func fileExists(src fs.FS, name string) bool {
	_, err := fs.Stat(src, name)
	return err == nil
}

func dirHasEntries(src fs.FS, name string) bool {
	entries, err := fs.ReadDir(src, name)
	return err == nil && len(entries) > 0
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
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".git" {
			dirs = append(dirs, path.Join(root, entry.Name()))
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
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, path.Join(root, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// listRuleFiles lists Windsurf rule files (.mdc, plus .md for tolerance) in root.
func listRuleFiles(src fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".mdc") || strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, path.Join(root, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func sortSkills(m map[string]*ir.Skill) []*ir.Skill {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ir.Skill, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

func sortRules(m map[string]*ir.Rule) []*ir.Rule {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ir.Rule, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

func sortCommands(m map[string]*ir.Command) []*ir.Command {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ir.Command, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

func sortHooks(m map[string]*ir.Hook) []*ir.Hook {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ir.Hook, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

func sortServers(m map[string]*ir.McpServer) []*ir.McpServer {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*ir.McpServer, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}
