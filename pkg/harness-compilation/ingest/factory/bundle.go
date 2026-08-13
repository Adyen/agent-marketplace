package factory

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// factoryPluginManifest is the Factory plugin manifest path.
const factoryPluginManifest = ".factory-plugin/plugin.json"

func init() {
	ingest.Register(New())
}

// Loader ingests Factory plugin and workspace layouts.
type Loader struct{}

// New returns a Factory ingester.
func New() *Loader { return &Loader{} }

func (l *Loader) Source() ingest.Harness { return ingest.HarnessFactory }

func (l *Loader) Detect(src fs.FS) bool {
	if _, err := fs.ReadFile(src, factoryPluginManifest); err == nil {
		return true
	}
	if hasBarePluginForm(src) {
		return true
	}
	return hasWorkspaceForm(src)
}

func (l *Loader) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	hasPlugin := hasPluginForm(src)
	hasBare := hasBarePluginForm(src)
	hasWorkspace := hasWorkspaceForm(src)
	if !hasPlugin && !hasBare && !hasWorkspace {
		return nil, fmt.Errorf("ingest/factory: no recognizable Factory layout found")
	}

	manifest, skillMeta, err := loadPluginManifest(src)
	if err != nil && !isNotExist(err) {
		return nil, fmt.Errorf("ingest/factory: load plugin manifest: %w", err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic

	seenSkills := map[string]bool{}
	seenCommands := map[string]bool{}
	seenDroids := map[string]bool{}
	seenRules := map[string]bool{}

	if hasPlugin || hasBare {
		added, addedDiags, err := loadSkillsFromRoot(src, "skills", skillMeta, seenSkills)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadCommandsFromRoot(src, "commands", seenCommands)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadDroidsFromRoot(src, "droids", seenDroids)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadRulesFromRoot(src, "rules", seenRules)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		if hooks, hookDiags, err := LoadHooks(src, "hooks/hooks.json"); err == nil {
			for _, hook := range hooks {
				resources = append(resources, hook)
			}
			diags = append(diags, hookDiags...)
		} else if !isNotExist(err) {
			return nil, fmt.Errorf("ingest/factory: load hooks: %w", err)
		}

		resources, diags = appendMcpServers(resources, diags, src, "mcp.json")
	}

	if hasWorkspace {
		added, addedDiags, err := loadSkillsFromRoot(src, ".factory/skills", nil, seenSkills)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadCommandsFromRoot(src, ".factory/commands", seenCommands)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadDroidsFromRoot(src, ".factory/droids", seenDroids)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		added, addedDiags, err = loadRulesFromRoot(src, ".factory/rules", seenRules)
		if err != nil {
			return nil, err
		}
		resources = append(resources, added...)
		diags = append(diags, addedDiags...)

		if !hasPlugin {
			if hooks, hookDiags, err := LoadHooks(src, ".factory/hooks/hooks.json"); err == nil {
				for _, hook := range hooks {
					resources = append(resources, hook)
				}
				diags = append(diags, hookDiags...)
			} else if !isNotExist(err) {
				return nil, fmt.Errorf("ingest/factory: load workspace hooks: %w", err)
			}

			resources, diags = appendMcpServers(resources, diags, src, ".factory/mcp.json")
		}
	}

	if !hasPlugin {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestFactoryManifestSynthesized,
			Severity: diag.SeverityInfo,
			Message:  "Factory source has no .factory-plugin/plugin.json; manifest synthesized",
		})
	}

	return &ingest.Bundle{
		Manifest:    manifest,
		Resources:   resources,
		Diagnostics: diags,
	}, nil
}

func appendMcpServers(resources []ir.Resource, diags []diag.Diagnostic, src fs.FS, path string) ([]ir.Resource, []diag.Diagnostic) {
	servers, err := LoadMcpServers(src, path)
	if err == nil {
		for _, server := range servers {
			resources = append(resources, server)
		}
		return resources, diags
	}
	if isNotExist(err) {
		return resources, diags
	}
	diags = append(diags, ingest.LoadFailure(diag.CodeIngestFactoryLoadMcpServersFailed, "MCP servers", path, path, err))
	return resources, diags
}

type pluginManifest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Skills      []pluginSkillEntry `json:"skills"`
}

type pluginSkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
}

type skillMetadata struct {
	Description string
	Triggers    []string
}

func loadPluginManifest(src fs.FS) (ir.Manifest, map[string]skillMetadata, error) {
	// Registry-supplied manifest: duplicate keys are rejected before decoding
	// because name/version and the per-skill description/triggers feed
	// ownership markers and output paths.
	var raw pluginManifest
	if err := ingest.LoadJSONManifest(src, factoryPluginManifest, &raw); err != nil {
		return ir.Manifest{}, nil, err
	}

	manifest := ir.Manifest{
		Name:        raw.Name,
		Description: raw.Description,
		Version:     raw.Version,
	}

	metadata := make(map[string]skillMetadata, len(raw.Skills))
	for _, entry := range raw.Skills {
		metadata[entry.Name] = skillMetadata{
			Description: entry.Description,
			Triggers:    append([]string(nil), entry.Triggers...),
		}
	}

	return manifest, metadata, nil
}

func loadSkillsFromRoot(src fs.FS, root string, metadata map[string]skillMetadata, seen map[string]bool) ([]ir.Resource, []diag.Diagnostic, error) {
	dirs, err := listSubdirs(src, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/factory: walk %s: %w", root, err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, dir := range dirs {
		name := dirName(dir)
		if seen[name] {
			continue
		}

		skill, err := LoadSkill(src, dir)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestFactoryLoadSkillFailed, "skill", dir, dir, err))
			continue
		}
		if meta, ok := metadata[skill.Name()]; ok {
			if skill.Description == "" {
				skill.Description = meta.Description
			}
			if len(skill.Triggers) == 0 && len(meta.Triggers) > 0 {
				skill.Triggers = append([]string(nil), meta.Triggers...)
			}
		}

		seen[skill.Name()] = true
		resources = append(resources, skill)
	}
	return resources, diags, nil
}

func loadCommandsFromRoot(src fs.FS, root string, seen map[string]bool) ([]ir.Resource, []diag.Diagnostic, error) {
	files, err := listMDFiles(src, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/factory: walk %s: %w", root, err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, path := range files {
		name := baseNameWithoutExt(path)
		if seen[name] {
			continue
		}
		command, err := LoadCommand(src, path)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestFactoryLoadCommandFailed, "command", path, path, err))
			continue
		}
		seen[command.Name()] = true
		resources = append(resources, command)
	}
	return resources, diags, nil
}

func loadDroidsFromRoot(src fs.FS, root string, seen map[string]bool) ([]ir.Resource, []diag.Diagnostic, error) {
	files, err := listMDFiles(src, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/factory: walk %s: %w", root, err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, path := range files {
		name := baseNameWithoutExt(path)
		if seen[name] {
			continue
		}
		subagent, subagentDiags, err := LoadSubagent(src, path)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestFactoryLoadSubagentFailed, "subagent", path, path, err))
			continue
		}
		diags = append(diags, subagentDiags...)
		seen[subagent.Name()] = true
		resources = append(resources, subagent)
	}
	return resources, diags, nil
}

func loadRulesFromRoot(src fs.FS, root string, seen map[string]bool) ([]ir.Resource, []diag.Diagnostic, error) {
	files, err := listRuleFiles(src, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("ingest/factory: walk %s: %w", root, err)
	}

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, path := range files {
		rule, err := LoadRule(src, path)
		if err != nil {
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestFactoryLoadRuleFailed, "rule", path, path, err))
			continue
		}
		if seen[rule.Name()] {
			continue
		}
		seen[rule.Name()] = true
		resources = append(resources, rule)
	}
	return resources, diags, nil
}

func hasPluginForm(src fs.FS) bool {
	_, err := fs.ReadFile(src, factoryPluginManifest)
	return err == nil
}

func hasWorkspaceForm(src fs.FS) bool {
	checks := []string{
		".factory/skills",
		".factory/commands",
		".factory/droids",
		".factory/hooks",
		".factory/rules",
	}
	for _, dir := range checks {
		if entries, err := fs.ReadDir(src, dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	_, err := fs.ReadFile(src, ".factory/mcp.json")
	return err == nil
}

func hasBarePluginForm(src fs.FS) bool {
	checks := []string{
		"skills",
		"commands",
		"droids",
		"hooks",
		"rules",
	}
	for _, dir := range checks {
		if entries, err := fs.ReadDir(src, dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	_, err := fs.ReadFile(src, "mcp.json")
	return err == nil
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "file does not exist") ||
		strings.Contains(err.Error(), "directory does not exist") ||
		strings.Contains(err.Error(), "no such file or directory")
}

func listSubdirs(src fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		path := root + "/" + entry.Name()
		if entry.IsDir() {
			dirs = append(dirs, path)
			continue
		}
		// Also resolve symlinks that point to directories.
		// os.DirFS-backed FS entries report symlinks as ModeSymlink, not IsDir.
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := fs.Stat(src, path); err == nil && info.IsDir() {
				dirs = append(dirs, path)
			}
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
			files = append(files, root+"/"+entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// listRuleFiles lists rule files in root. Factory rules are plain Markdown
// (.md), but .mdc files (Cursor/Windsurf-authored) are also accepted so a
// bare rules/ directory ingests correctly regardless of authoring harness.
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
		if strings.HasSuffix(entry.Name(), ".md") || strings.HasSuffix(entry.Name(), ".mdc") {
			files = append(files, root+"/"+entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}
