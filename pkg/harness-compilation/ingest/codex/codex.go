// Package codex ingests documented Codex project layouts.
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const kindKey = "x-agent-marketplace-kind"

type ingester struct{}

func init() { ingest.Register(ingester{}) }

func (ingester) Source() ingest.Harness { return ingest.HarnessCodex }

func (ingester) Detect(src fs.FS) bool {
	info, err := fs.Stat(src, ".codex")
	return err == nil && info.IsDir()
}

func (i ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func Detect(src fs.FS) bool { return ingester{}.Detect(src) }

func LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func load(src fs.FS) (*ingest.Bundle, error) {
	if !Detect(src) {
		return nil, fmt.Errorf("ingest/codex: no .codex source marker")
	}
	resources, err := loadSkills(src)
	if err != nil {
		return nil, err
	}
	for _, loader := range []func(fs.FS) ([]ir.Resource, error){loadSubagents, loadHooks, loadMCP, loadPrompts} {
		loaded, err := loader(src)
		if err != nil {
			return nil, err
		}
		resources = append(resources, loaded...)
	}
	return &ingest.Bundle{Manifest: ir.Manifest{Name: "codex-workspace"}, Resources: resources}, nil
}

func loadSkills(src fs.FS) ([]ir.Resource, error) {
	entries, err := fs.ReadDir(src, ".agents/skills")
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	var resources []ir.Resource
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skill, err := bundle.LoadSkill(src, path.Join(".agents/skills", entry.Name()))
		if err != nil {
			return nil, err
		}
		if marker, ok := skill.Extra[kindKey]; ok && marker == "command" {
			delete(skill.Extra, kindKey)
			resources = append(resources, ir.NewCommand(skill.Name(), skill.Description, skill.Body, skill.Extra, skill.Source))
			continue
		}
		resources = append(resources, skill)
	}
	return resources, nil
}

type subagentDocument struct {
	Name         string `toml:"name"`
	Description  string `toml:"description"`
	Model        string `toml:"model"`
	Effort       string `toml:"model_reasoning_effort"`
	Instructions string `toml:"developer_instructions"`
}

func loadSubagents(src fs.FS) ([]ir.Resource, error) {
	files, err := filesWithSuffix(src, ".codex/agents", ".toml")
	if err != nil {
		return nil, err
	}
	resources := make([]ir.Resource, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			return nil, err
		}
		var doc subagentDocument
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("ingest/codex: parse %s: %w", file, err)
		}
		name := doc.Name
		if name == "" {
			name = strings.TrimSuffix(path.Base(file), ".toml")
		}
		resources = append(resources, ir.NewSubagent(name, doc.Description, doc.Instructions, doc.Model, nil, doc.Effort, nil, "", nil, ir.SourceRef{File: file, Line: 1}))
	}
	return resources, nil
}

type hooksDocument struct {
	Hooks map[string][]hookEntry `json:"hooks"`
}
type hookEntry struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookHandler `json:"hooks"`
}
type hookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

func loadHooks(src fs.FS) ([]ir.Resource, error) {
	const file = ".codex/hooks.json"
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc hooksDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("ingest/codex: parse %s: %w", file, err)
	}
	events := make([]string, 0, len(doc.Hooks))
	for event := range doc.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	var resources []ir.Resource
	for _, event := range events {
		for entryIndex, entry := range doc.Hooks[event] {
			for handlerIndex, handler := range entry.Hooks {
				name := fmt.Sprintf("%s-%d-%d", strings.ToLower(event), entryIndex+1, handlerIndex+1)
				resources = append(resources, ir.NewHookWithMatcher(name, event, entry.Matcher, handler.Command, handler.Type, nil, ir.SourceRef{File: file, Line: 1}))
			}
		}
	}
	return resources, nil
}

type configDocument struct {
	MCP map[string]mcpDocument `toml:"mcp_servers"`
}
type mcpDocument struct {
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
}

func loadMCP(src fs.FS) ([]ir.Resource, error) {
	const file = ".codex/config.toml"
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc configDocument
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("ingest/codex: parse %s: %w", file, err)
	}
	names := make([]string, 0, len(doc.MCP))
	for name := range doc.MCP {
		names = append(names, name)
	}
	sort.Strings(names)
	resources := make([]ir.Resource, 0, len(names))
	for _, name := range names {
		server := doc.MCP[name]
		options := []ir.McpServerOption{}
		if server.URL != "" {
			options = append(options, ir.WithMcpURL(server.URL))
		}
		resources = append(resources, ir.NewMcpServerWithOptions(name, server.Command, server.Args, server.Env, nil, ir.SourceRef{File: file, Line: 1}, options...))
	}
	return resources, nil
}

func loadPrompts(src fs.FS) ([]ir.Resource, error) {
	files, err := filesWithSuffix(src, ".codex/prompts", ".md")
	if err != nil {
		return nil, err
	}
	resources := make([]ir.Resource, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(path.Base(file), ".md")
		resources = append(resources, ir.NewCommand(name, "Deprecated Codex prompt", string(raw), nil, ir.SourceRef{File: file, Line: 1}))
	}
	return resources, nil
}

func filesWithSuffix(src fs.FS, dir, suffix string) ([]string, error) {
	entries, err := fs.ReadDir(src, dir)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			files = append(files, path.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

func isMissing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
