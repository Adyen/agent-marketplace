// Package codex ingests documented Codex project layouts.
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const kindKey = "x-agent-marketplace-kind"

type ingester struct{}

func init() { ingest.Register(ingester{}) }

func (ingester) Source() ingest.Harness { return ingest.HarnessCodex }

func (ingester) Detect(src fs.FS) bool {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetCodex)
	if !ok {
		return false
	}
	for _, partition := range layout.Partitions {
		if !partition.InstallationEvidence {
			continue
		}
		info, err := fs.Stat(src, partition.Prefix)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (i ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func Detect(src fs.FS) bool { return ingester{}.Detect(src) }

func LoadBundle(src fs.FS) (*ingest.Bundle, error) { return load(src) }

func load(src fs.FS) (*ingest.Bundle, error) {
	if !Detect(src) {
		return nil, fmt.Errorf("ingest/codex: no .codex source marker")
	}
	slog.Info("codex ingest start", "source", ingest.HarnessCodex)
	var resources []ir.Resource
	diagnostics := []diag.Diagnostic{{Code: diag.CodeIngestCodexManifestSynthesized, Severity: diag.SeverityInfo, Resource: "bundle", Message: "Codex workspace has no bundle manifest; manifest synthesized"}}
	for _, loader := range []func(fs.FS) ([]ir.Resource, []diag.Diagnostic){loadSkills, loadSubagents, loadHooks, loadMCP, loadPrompts} {
		loaded, loaderDiagnostics := loader(src)
		resources = append(resources, loaded...)
		diagnostics = append(diagnostics, loaderDiagnostics...)
	}
	resources, duplicateDiagnostics := suppressDuplicates(resources)
	diagnostics = append(diagnostics, duplicateDiagnostics...)
	slog.Info("codex ingest complete", "source", ingest.HarnessCodex, "resources", len(resources), "diagnostics", len(diagnostics))
	return &ingest.Bundle{Manifest: ir.Manifest{Name: "codex-workspace"}, Resources: resources, Diagnostics: diagnostics}, nil
}

func loadSkills(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	root, err := codexResourceRoot(ir.KindSkill)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, []diag.Diagnostic{loadFailure(root, err)}
	}
	var resources []ir.Resource
	var diagnostics []diag.Diagnostic
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile, err := codexResourceFile(ir.KindSkill, entry.Name())
		if err != nil {
			diagnostics = append(diagnostics, loadFailure("codex layout", err))
			continue
		}
		skill, err := bundle.LoadSkill(src, path.Dir(skillFile))
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(skillFile, err))
			continue
		}
		if marker, ok := skill.Extra[kindKey]; ok && marker == "command" {
			delete(skill.Extra, kindKey)
			resources = append(resources, ir.NewCommand(skill.Name(), skill.Description, skill.Body, skill.Extra, skill.Source))
			continue
		}
		resources = append(resources, skill)
	}
	return resources, diagnostics
}

type subagentDocument struct {
	Name         string `toml:"name"`
	Description  string `toml:"description"`
	Model        string `toml:"model"`
	Effort       string `toml:"model_reasoning_effort"`
	Instructions string `toml:"developer_instructions"`
	SandboxMode  string `toml:"sandbox_mode"`
}

func loadSubagents(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	root, err := codexResourceRoot(ir.KindSubagent)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	files, err := filesWithSuffix(src, root, ".toml")
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(root, err)}
	}
	resources := make([]ir.Resource, 0, len(files))
	var diagnostics []diag.Diagnostic
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		var doc subagentDocument
		if err := toml.Unmarshal(raw, &doc); err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		name := doc.Name
		if name == "" {
			name = strings.TrimSuffix(path.Base(file), ".toml")
		}
		subagent := ir.NewSubagent(name, doc.Description, doc.Instructions, doc.Model, nil, doc.Effort, nil, "", nil, ir.SourceRef{File: file, Line: 1})
		if doc.SandboxMode != "" {
			subagent.Ext.Codex = &extpkg.Codex{SandboxMode: doc.SandboxMode}
		}
		resources = append(resources, subagent)
	}
	return resources, diagnostics
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

func loadHooks(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	file, err := codexAggregateFile(ir.KindHook)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	var doc hooksDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
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
	Command           string            `toml:"command"`
	Args              []string          `toml:"args"`
	Env               map[string]string `toml:"env"`
	URL               string            `toml:"url"`
	HTTPHeaders       map[string]string `toml:"http_headers"`
	Enabled           *bool             `toml:"enabled"`
	EnvHTTPHeaders    map[string]string `toml:"env_http_headers"`
	BearerTokenEnvVar string            `toml:"bearer_token_env_var"`
}

func loadMCP(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	file, err := codexAggregateFile(ir.KindMcpServer)
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure("codex layout", err)}
	}
	raw, err := fs.ReadFile(src, file)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
	}
	var doc configDocument
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, []diag.Diagnostic{loadFailure(file, err)}
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
		if server.HTTPHeaders != nil {
			options = append(options, ir.WithMcpHeaders(server.HTTPHeaders))
		}
		if server.Enabled != nil {
			options = append(options, ir.WithMcpDisabled(!*server.Enabled))
		}
		resource := ir.NewMcpServerWithOptions(name, server.Command, server.Args, server.Env, nil, ir.SourceRef{File: file, Line: 1}, options...)
		if server.BearerTokenEnvVar != "" || server.EnvHTTPHeaders != nil {
			resource.Ext.Codex = &extpkg.Codex{BearerTokenEnvVar: server.BearerTokenEnvVar, EnvHTTPHeaders: server.EnvHTTPHeaders}
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

func loadPrompts(src fs.FS) ([]ir.Resource, []diag.Diagnostic) {
	files, err := filesWithSuffix(src, ".codex/prompts", ".md")
	if err != nil {
		return nil, []diag.Diagnostic{loadFailure(".codex/prompts", err)}
	}
	resources := make([]ir.Resource, 0, len(files))
	diagnostics := make([]diag.Diagnostic, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(src, file)
		if err != nil {
			diagnostics = append(diagnostics, loadFailure(file, err))
			continue
		}
		name := strings.TrimSuffix(path.Base(file), ".md")
		resources = append(resources, ir.NewCommand(name, "Deprecated Codex prompt", string(raw), nil, ir.SourceRef{File: file, Line: 1}))
		diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexPromptDeprecated, Severity: diag.SeverityWarning, Resource: name, Message: "deprecated Codex prompt ingested as a command", Source: diag.SourceRef{File: file, Line: 1}})
	}
	return resources, diagnostics
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

func codexResourceRoot(kind ir.ResourceKind) (string, error) {
	root, ok := targetmeta.NativeResourceRoot(targetmeta.TargetCodex, kind)
	if !ok {
		return "", fmt.Errorf("native resource root for %s is not registered", kind)
	}
	return root, nil
}

func codexResourceFile(kind ir.ResourceKind, name string) (string, error) {
	file, ok := targetmeta.NativeResourcePath(targetmeta.TargetCodex, kind, name)
	if !ok {
		return "", fmt.Errorf("native resource path for %s is not registered", kind)
	}
	return file, nil
}

func codexAggregateFile(kind ir.ResourceKind) (string, error) {
	file, ok := targetmeta.NativeAggregatePath(targetmeta.TargetCodex, kind)
	if !ok {
		return "", fmt.Errorf("native aggregate path for %s is not registered", kind)
	}
	return file, nil
}

func isMissing(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func loadFailure(file string, err error) diag.Diagnostic {
	slog.Error("codex ingest resource skipped", "file", file, "code", diag.CodeIngestCodexLoadResourceFailed)
	return diag.Diagnostic{Code: diag.CodeIngestCodexLoadResourceFailed, Severity: diag.SeverityError, Resource: path.Base(file), Message: "malformed Codex resource was skipped", Hint: "fix the reported native file; independent valid resources were retained", Source: diag.SourceRef{File: file, Line: 1}}
}

func suppressDuplicates(resources []ir.Resource) ([]ir.Resource, []diag.Diagnostic) {
	counts := make(map[string]int, len(resources))
	for _, resource := range resources {
		counts[resource.Kind().String()+"\x00"+resource.Name()]++
	}
	kept := resources[:0]
	var diagnostics []diag.Diagnostic
	reported := map[string]bool{}
	for _, resource := range resources {
		key := resource.Kind().String() + "\x00" + resource.Name()
		if counts[key] == 1 {
			kept = append(kept, resource)
			continue
		}
		if !reported[key] {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeIngestCodexDuplicateIdentity, Severity: diag.SeverityError, Resource: resource.Name(), Message: "duplicate Codex resource identity was suppressed without choosing a winner"})
			reported[key] = true
		}
	}
	return kept, diagnostics
}
