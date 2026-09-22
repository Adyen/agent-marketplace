// Package claude implements the Ingester for Claude Code source layouts.
// It wraps the existing bundle/ loaders for the Claude-plugin form
// and also supports the .claude/ workspace form.
package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// claudePluginManifest is the Claude-plugin manifest path.
const claudePluginManifest = ".claude-plugin/plugin.json"

func init() {
	ingest.Register(&claudeIngester{})
}

// claudeIngester handles both Claude-plugin form (.claude-plugin/plugin.json)
// and Claude workspace form (.claude/).
type claudeIngester struct{}

func (c *claudeIngester) Source() ingest.Harness {
	return ingest.HarnessClaude
}

// Detect returns true if the filesystem contains a Claude layout.
// It checks for:
//  1. .claude-plugin/plugin.json (plugin form, highest priority)
//  2. .claude/ directory with at least one resource subdirectory (workspace form)
//  3. Bare form: root-level skills/commands/agents/hooks/.mcp.json
//  4. Bare skills directory: immediate subdirectories with SKILL.md, or the directory itself contains SKILL.md
func (c *claudeIngester) Detect(src fs.FS) bool {
	// Plugin form: .claude-plugin/plugin.json
	if _, err := fs.ReadFile(src, claudePluginManifest); err == nil {
		return true
	}
	// Workspace form: .claude/ with at least one resource subdirectory
	if hasClaudeWorkspaceForm(src) {
		return true
	}
	// Bare form: root-level skills/commands/agents/hooks/.mcp.json (Claude-plugin without .claude-plugin/)
	if hasBarePluginForm(src) {
		return true
	}
	// Bare skills directory: directory contains subdirectories with SKILL.md
	// or SKILL.md at root (a single skill directory). This handles configs
	// where path points at a skills/ subdirectory directly rather than a
	// bundle root.
	if hasBareSkillsDirForm(src) {
		return true
	}
	return false
}

// DetectStrict implements ingest.StrictDetector. It is like Detect but
// excludes the bare skills directory form (case 4). This prevents
// sub-bundle scanning from treating individual skill directories as
// independent bundle roots.
func (c *claudeIngester) DetectStrict(src fs.FS) bool {
	// Plugin form: .claude-plugin/plugin.json
	if _, err := fs.ReadFile(src, claudePluginManifest); err == nil {
		return true
	}
	// Workspace form: .claude/ with at least one resource subdirectory
	if hasClaudeWorkspaceForm(src) {
		return true
	}
	// Bare form: root-level skills/commands/agents/hooks/.mcp.json
	if hasBarePluginForm(src) {
		return true
	}
	return false
}

// hasClaudeWorkspaceForm checks for .claude/ directory with resource subdirs.
func hasClaudeWorkspaceForm(src fs.FS) bool {
	if _, err := fs.Stat(src, ".claude"); err == nil {
		if _, err := fs.Stat(src, ".mcp.json"); err == nil {
			return true
		}
	}
	if _, err := fs.Stat(src, ".claude.json"); err == nil {
		return true
	}
	requiredDirs := []string{".claude/skills", ".claude/agents", ".claude/commands", ".claude/hooks"}
	for _, d := range requiredDirs {
		if entries, err := fs.ReadDir(src, d); err == nil && len(entries) > 0 {
			return true
		}
	}
	// Also check .claude/.mcp.json
	if _, err := fs.ReadFile(src, ".claude/.mcp.json"); err == nil {
		return true
	}
	return false
}

// hasBarePluginForm checks for root-level Claude resource directories.
func hasBarePluginForm(src fs.FS) bool {
	// If another plugin-form harness marker is present, root-level resources are
	// more likely siblings for that harness than a bare Claude plugin.
	if _, err := fs.ReadFile(src, ".factory-plugin/plugin.json"); err == nil {
		return false
	}
	if _, err := fs.ReadFile(src, ".cursor-plugin/plugin.json"); err == nil {
		return false
	}
	// droids/ is Factory-specific; defer to the factory ingester rather than
	// claiming this as a bare Claude plugin and silently missing droids/mcp.json.
	if entries, err := fs.ReadDir(src, "droids"); err == nil && len(entries) > 0 {
		return false
	}

	// Check for root-level skills/, commands/, agents/, hooks/
	checks := []string{"skills", "commands", "agents", "hooks"}
	for _, d := range checks {
		if entries, err := fs.ReadDir(src, d); err == nil && len(entries) > 0 {
			return true
		}
	}
	if _, err := fs.ReadFile(src, ".mcp.json"); err == nil {
		return true
	}
	return false
}

// hasBareSkillsDirForm reports whether the filesystem represents a bare
// skills directory — either a directory of skill subdirectories (each
// containing SKILL.md) or a single-skill directory (SKILL.md at root).
// This handles configs where path points at a skills/ subdirectory
// directly rather than a bundle root containing skills/ as a child.
func hasBareSkillsDirForm(src fs.FS) bool {
	// Single skill at root: the directory itself is a skill directory.
	if _, err := fs.ReadFile(src, "SKILL.md"); err == nil {
		return true
	}
	// Directory of skill subdirectories: immediate subdirs containing SKILL.md.
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := fs.ReadFile(src, e.Name()+"/SKILL.md"); err == nil {
			return true
		}
	}
	return false
}

// LoadBundle reads all resources from the given filesystem and returns
// them as an IR Bundle. It tries plugin form first, then workspace form,
// then bare skills directory form.
func (c *claudeIngester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	return LoadBundleWithOptions(src, LoadOptions{})
}

type LoadOptions struct {
	LegacyBareMCP bool
}

func LoadBundleWithOptions(src fs.FS, opts LoadOptions) (*ingest.Bundle, error) {
	// Try plugin form first (.claude-plugin/plugin.json or bare form).
	// The explicit plugin manifest is Detect's highest-priority marker, so
	// honor it here too to keep Detect and LoadBundle in agreement.
	if _, err := fs.ReadFile(src, claudePluginManifest); err == nil {
		return loadPluginForm(src, false)
	}
	if hasBarePluginForm(src) && !hasClaudeWorkspaceForm(src) {
		return loadPluginForm(src, !opts.LegacyBareMCP)
	}
	// Try workspace form
	if hasClaudeWorkspaceForm(src) {
		return loadWorkspaceForm(src)
	}
	// Try bare skills directory form (skills-only dir or single skill dir)
	if hasBareSkillsDirForm(src) {
		return loadBareSkillsDirForm(src)
	}
	return nil, fmt.Errorf("ingest/claude: no recognizable Claude layout found")
}

// loadPluginForm loads resources from the Claude-plugin layout:
//   - skills/<name>/ → LoadSkill
//   - commands/<name>.md → LoadCommand
//   - agents/<name>.md → LoadSubagent
//   - hooks/hooks.json → LoadHooks
//   - .mcp.json → LoadMcpServers
//
// This is the same layout used by harnesscompile.CompileBundle.
func loadPluginForm(src fs.FS, bareMCP bool) (*ingest.Bundle, error) {
	var resources []ir.Resource
	var diags []diag.Diagnostic

	// Load manifest if present. A present-but-invalid manifest is fatal: it is
	// registry-supplied JSON whose name/version feed ownership markers and
	// output paths, so it must never be silently reduced to an empty manifest.
	manifest := ir.Manifest{}
	var m struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Version     string         `json:"version"`
		Extra       map[string]any `json:"-"`
	}
	switch err := ingest.LoadJSONManifest(src, claudePluginManifest, &m); {
	case err == nil:
		manifest.Name = m.Name
		manifest.Description = m.Description
		manifest.Version = m.Version
	case isNotExist(err):
		// No manifest in this layout; leave it synthesized.
	default:
		return nil, fmt.Errorf("ingest/claude: parse %s: %w", claudePluginManifest, err)
	}

	// 1. Skills
	skillDirs, err := listSubdirs(src, "skills")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk skills: %w", err)
	}
	for _, dir := range skillDirs {
		skill, err := bundle.LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSkillFailed, "skill", dir, dir, err))
			continue
		}
		resources = append(resources, skill)
	}

	// 2. Commands
	cmdFiles, err := listMDFiles(src, "commands")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk commands: %w", err)
	}
	for _, f := range cmdFiles {
		cmd, err := bundle.LoadCommand(src, f)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadCommandFailed, "command", f, f, err))
			continue
		}
		resources = append(resources, cmd)
	}

	// 3. Subagents
	agentFiles, err := listMDFiles(src, "agents")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk agents: %w", err)
	}
	for _, f := range agentFiles {
		sub, err := bundle.LoadSubagent(src, f)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSubagentFailed, "subagent", f, f, err))
			continue
		}
		resources = append(resources, sub)
	}

	// 4. Hooks
	hooks, hookDiags, err := bundle.LoadHooks(src, "hooks/hooks.json")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: load hooks: %w", err)
	}
	if err == nil {
		for _, d := range hookDiags {
			diags = append(diags, *d)
		}
		for _, h := range hooks {
			resources = append(resources, h)
		}
	}

	// 5. MCP servers
	mcpPath := ".mcp.json"
	if bareMCP {
		if _, err := fs.Stat(src, mcpPath); errors.Is(err, fs.ErrNotExist) {
			mcpPath = "mcp.json"
		}
	}
	servers, err := bundle.LoadMcpServers(src, mcpPath)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: load %s: %w", mcpPath, err)
	}
	if err == nil {
		for _, s := range servers {
			resources = append(resources, s)
		}
	}

	layout := "claude bare-plugin"
	if _, err := fs.Stat(src, claudePluginManifest); err == nil {
		layout = "claude plugin"
	}
	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestClaudeUnconsumedConfig, layout, "hooks/hooks.json", mcpPath)...)
	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

// loadWorkspaceForm loads resources from the .claude/ workspace layout:
//   - .claude/skills/<name>/ → LoadSkill (with path prefix .claude/)
//   - .claude/agents/<name>.md → LoadSubagent
//   - .claude/commands/<name>.md → LoadCommand
//   - .claude/hooks/hooks.json → LoadHooks
//   - .claude/.mcp.json → LoadMcpServers
func loadWorkspaceForm(src fs.FS) (*ingest.Bundle, error) {
	var resources []ir.Resource
	var diags []diag.Diagnostic

	manifest := ir.Manifest{} // synthesized

	// 1. Skills
	skillDirs, err := listSubdirs(src, ".claude/skills")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk .claude/skills: %w", err)
	}
	for _, dir := range skillDirs {
		skill, err := bundle.LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSkillFailed, "skill", dir, dir, err))
			continue
		}
		resources = append(resources, skill)
	}

	// 2. Commands
	cmdFiles, err := listMDFiles(src, ".claude/commands")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk .claude/commands: %w", err)
	}
	for _, f := range cmdFiles {
		cmd, err := bundle.LoadCommand(src, f)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadCommandFailed, "command", f, f, err))
			continue
		}
		resources = append(resources, cmd)
	}

	// 3. Subagents
	agentFiles, err := listMDFiles(src, ".claude/agents")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: walk .claude/agents: %w", err)
	}
	for _, f := range agentFiles {
		sub, err := bundle.LoadSubagent(src, f)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSubagentFailed, "subagent", f, f, err))
			continue
		}
		resources = append(resources, sub)
	}

	// 4. Hooks
	hooks, hookDiags, err := bundle.LoadHooks(src, ".claude/hooks/hooks.json")
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: load .claude/hooks: %w", err)
	}
	if err == nil {
		for _, d := range hookDiags {
			diags = append(diags, *d)
		}
		for _, h := range hooks {
			resources = append(resources, h)
		}
	}

	// 5. MCP servers
	mcpPath := ".claude/.mcp.json"
	for _, candidate := range []string{".mcp.json", ".claude.json"} {
		if _, err := fs.Stat(src, candidate); err == nil {
			mcpPath = candidate
			break
		}
	}
	servers, err := bundle.LoadMcpServers(src, mcpPath)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/claude: load %s: %w", mcpPath, err)
	}
	if err == nil {
		for _, s := range servers {
			resources = append(resources, s)
		}
	}

	if len(resources) == 0 && len(diags) == 0 {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestClaudeManifestSynthesized,
			Severity: diag.SeverityInfo,
			Message:  "workspace form has no resources; manifest synthesized",
		})
	} else if len(resources) > 0 {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestClaudeManifestSynthesized,
			Severity: diag.SeverityInfo,
			Message:  "workspace form: manifest synthesized (no .claude-plugin/plugin.json found)",
		})
	}

	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestClaudeUnconsumedConfig, "claude workspace", ".claude/hooks/hooks.json", mcpPath)...)
	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

// loadBareSkillsDirForm loads resources from a bare skills directory.
// Two sub-forms are handled:
//
//  1. Multi-skill directory: root contains subdirectories, each with
//     a SKILL.md file. Treated like a skills/ directory without the
//     parent bundle root — each subdirectory is loaded as a skill.
//  2. Single-skill directory: root itself contains SKILL.md. Loaded
//     via bundle.LoadRootSkill which requires a frontmatter name field.
func loadBareSkillsDirForm(src fs.FS) (*ingest.Bundle, error) {
	var resources []ir.Resource
	var diags []diag.Diagnostic

	manifest := ir.Manifest{} // synthesized

	// Single skill at root: SKILL.md lives at the filesystem root.
	// When a root SKILL.md exists, this is a single-skill directory —
	// return it immediately without scanning for sibling subdirectories
	// to avoid double-publishing in mixed layouts.
	if _, err := fs.ReadFile(src, "SKILL.md"); err == nil {
		skill, err := bundle.LoadRootSkill(src)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSkillFailed, "root skill", ".", ".", err))
			diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestClaudeUnconsumedConfig, "claude bare-skills")...)
			return &ingest.Bundle{Manifest: manifest, Diagnostics: diags}, nil
		}
		diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestClaudeUnconsumedConfig, "claude bare-skills")...)
		return &ingest.Bundle{Manifest: manifest, Resources: []ir.Resource{skill}, Diagnostics: diags}, nil
	}

	// No root SKILL.md: scan for skill subdirectories at root level.
	skillDirs, err := listSkillSubdirs(src)
	if err != nil {
		return nil, fmt.Errorf("ingest/claude: scan bare skills dir: %w", err)
	}
	for _, dir := range skillDirs {
		skill, err := bundle.LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestClaudeLoadSkillFailed, "skill", dir, dir, err))
			continue
		}
		resources = append(resources, skill)
	}

	if len(resources) == 0 && len(diags) == 0 {
		return nil, fmt.Errorf("ingest/claude: bare skills dir has no loadable skills")
	}

	diags = append(diags, ingest.UnconsumedConfigs(src, diag.CodeIngestClaudeUnconsumedConfig, "claude bare-skills")...)
	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

// listSkillSubdirs returns immediate subdirectories that contain SKILL.md.
func listSkillSubdirs(src fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := fs.ReadFile(src, path.Join(e.Name(), "SKILL.md")); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// --- FS walking helpers ---

// isNotExist reports whether an error is a "file does not exist" error.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// listSubdirs returns the immediate subdirectories of root within src.
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

// listMDFiles returns all .md files in root within src.
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
