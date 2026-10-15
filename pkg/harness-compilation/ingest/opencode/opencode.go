// Package opencode reads OpenCode resources without loading plugins or running
// commands. An explicit source may be a workspace or a native config directory.
package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	ext "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

const maxResourceBytes = 4 << 20

var errReadRefused = errors.New("native resource read refused")

type Ingester struct{}

func init() { ingest.Register(Ingester{}) }

func (Ingester) Source() ingest.Harness                       { return "opencode" }
func (Ingester) Detect(src fs.FS) bool                        { return Detect(src) }
func (Ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) { return LoadBundle(src) }

// markerEntries are the .opencode entries that only OpenCode content creates.
// OpenCode itself writes dependency manifests such as package.json into a
// configuration directory, so the directory alone is not a source marker.
var markerEntries = []string{"skills", "skill", "commands", "command", "agents", "agent", "plugins", "plugin", "opencode.json", "opencode.jsonc"}

// Detect reports whether src carries an OpenCode source marker: an .opencode
// directory holding an OpenCode resource location, hook declarations, or a
// configuration file. Detection reads directory listings only and never
// follows a link, so probing a source that another layout claims cannot
// refuse that source on a link inside, or at, .opencode.
func Detect(src fs.FS) bool {
	if !listedDir(src, ".", ".opencode") {
		return false
	}
	entries, err := fs.ReadDir(src, ".opencode")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if slices.Contains(markerEntries, entry.Name()) {
			return true
		}
	}
	return listedDir(src, ".opencode", "hooks") && listedDir(src, ".opencode/hooks", "agent-marketplace")
}

// listedDir reports whether dir lists name as a directory, not a link to one.
func listedDir(src fs.FS, dir, name string) bool {
	entries, err := fs.ReadDir(src, dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry.IsDir()
		}
	}
	return false
}

// hasWorkspaceRoot reports whether src nests its native files under .opencode
// rather than being a direct configuration directory.
func hasWorkspaceRoot(src fs.FS) bool {
	info, err := fs.Stat(src, ".opencode")
	return err == nil && info.IsDir()
}

// boundedFS applies the limit at Open, including skill loaders which read
// through bundle.LoadSkill. A growing opened file cannot bypass the limit.
type boundedFS struct{ fs.FS }
type boundedFile struct {
	fs.File
	remaining int64
}

func (f boundedFS) Open(name string) (fs.File, error) {
	info, err := fs.Stat(f.FS, name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && (!info.Mode().IsRegular() || info.Size() > maxResourceBytes) {
		return nil, errReadRefused
	}
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if opened.IsDir() {
		return file, nil
	}
	if !opened.Mode().IsRegular() || opened.Size() > maxResourceBytes {
		_ = file.Close()
		return nil, errReadRefused
	}
	return &boundedFile{File: file, remaining: maxResourceBytes}, nil
}
func (f *boundedFile) Read(p []byte) (int, error) {
	if f.remaining == 0 {
		var extra [1]byte
		n, err := f.File.Read(extra[:])
		if n > 0 {
			return 0, errReadRefused
		}
		return 0, err
	}
	if int64(len(p)) > f.remaining {
		p = p[:f.remaining]
	}
	n, err := f.File.Read(p)
	f.remaining -= int64(n)
	return n, err
}

func LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	root := "."
	if hasWorkspaceRoot(src) {
		root = ".opencode"
	}
	limited := boundedFS{src}
	b := &ingest.Bundle{Manifest: ir.Manifest{Name: "opencode-workspace"}}
	for _, kind := range []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent} {
		dirs := []string{"skills", "skill"}
		if kind == ir.KindCommand {
			dirs = []string{"commands", "command"}
		}
		if kind == ir.KindSubagent {
			dirs = []string{"agents", "agent"}
		}
		seen := map[string]bool{}
		for _, dir := range dirs {
			base := path.Join(root, dir)
			entries, err := fs.ReadDir(src, base)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				b.Diagnostics = append(b.Diagnostics, failure(base, err))
				continue
			}
			for _, entry := range entries {
				file := path.Join(base, entry.Name())
				name := strings.TrimSuffix(entry.Name(), ".md")
				if kind == ir.KindSkill {
					if !entry.IsDir() {
						continue
					}
					file = path.Join(file, "SKILL.md")
					name = entry.Name()
				} else if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				if seen[name] {
					continue
				}
				seen[name] = true
				resource, err := loadMarkdown(limited, file, kind)
				if err != nil {
					b.Diagnostics = append(b.Diagnostics, failure(file, err))
					continue
				}
				b.Resources = append(b.Resources, resource)
			}
		}
	}
	// Workspace MCP configuration is at the workspace root. A direct native
	// directory keeps it next to its resources. JSONC overlays JSON natively.
	servers := map[string]*ir.McpServer{}
	inline := map[ir.ResourceKind]map[string]bool{}
	configFiles := []string{"opencode.json", "opencode.jsonc"}
	if root != "." {
		configFiles = append(configFiles, path.Join(root, "opencode.json"), path.Join(root, "opencode.jsonc"))
	}
	for _, file := range configFiles {
		data, err := fs.ReadFile(limited, file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			b.Diagnostics = append(b.Diagnostics, failure(file, err))
			continue
		}
		doc, err := jsonc.ParseWithOptions(file, data, jsonc.ParseOptions{AllowTrailingCommas: true})
		if err != nil {
			b.Diagnostics = append(b.Diagnostics, failure(file, err))
			continue
		}
		// Inline executable resources override Markdown natively. Until they
		// have a typed ingestion path, report them and omit affected identities.
		for _, section := range []struct {
			key  string
			kind ir.ResourceKind
		}{{"agent", ir.KindSubagent}, {"command", ir.KindCommand}} {
			if node, present := doc.Root().Member(section.key); present {
				if node.Kind() == jsonc.KindObject && len(node.Keys()) == 0 {
					continue
				}
				b.Diagnostics = append(b.Diagnostics, failure(file, fs.ErrInvalid))
				if inline[section.kind] == nil {
					inline[section.kind] = map[string]bool{}
				}
				if node.Kind() != jsonc.KindObject {
					inline[section.kind]["*"] = true
				} else {
					for _, name := range node.Keys() {
						inline[section.kind][name] = true
					}
				}
			}
		}
		mcp, ok := doc.Root().Member("mcp")
		if !ok {
			continue
		}
		if mcp.Kind() != jsonc.KindObject {
			b.Diagnostics = append(b.Diagnostics, failure(file, fmt.Errorf("invalid MCP container")))
			continue
		}
		for _, name := range mcp.Keys() {
			delete(servers, name) // A refused overlay must not resurrect an older entry.
			node, _ := mcp.Member(name)
			server, err := loadMCP(file, name, node)
			if err != nil {
				b.Diagnostics = append(b.Diagnostics, failure(file, err))
				continue
			}
			servers[name] = server
		}
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.Resources = append(b.Resources, servers[name])
	}
	hooks, diagnostics := loadHooks(src, root)
	b.Resources = append(b.Resources, hooks...)
	b.Diagnostics = append(b.Diagnostics, diagnostics...)
	// Filename aliases have already been resolved. Distinct files can still
	// claim the same declared identity, so do not pick an arbitrary winner.
	type resourceIdentity struct {
		kind ir.ResourceKind
		name string
	}
	counts := map[resourceIdentity]int{}
	for _, resource := range b.Resources {
		counts[resourceIdentity{resource.Kind(), resource.Name()}]++
	}
	kept := b.Resources[:0]
	for _, resource := range b.Resources {
		if inline[resource.Kind()][resource.Name()] || inline[resource.Kind()]["*"] {
			continue
		}
		identity := resourceIdentity{resource.Kind(), resource.Name()}
		if counts[identity] > 1 {
			b.Diagnostics = append(b.Diagnostics, diag.Diagnostic{Code: diag.CodeIngestOpenCodeDuplicateIdentity, Severity: diag.SeverityError, Resource: resource.Name(), Message: "Distinct OpenCode source files claim the same resource identity; resources omitted"})
			continue
		}
		kept = append(kept, resource)
	}
	b.Resources = kept
	return b, nil
}

func failure(file string, err error) diag.Diagnostic {
	code := diag.CodeIngestOpenCodeInvalidResource
	if errors.Is(err, errReadRefused) {
		code = diag.CodeIngestOpenCodeReadRefused
	}
	return diag.Diagnostic{Code: code, Severity: diag.SeverityError, Source: diag.SourceRef{File: file}, Message: "OpenCode resource could not be read or decoded; resource omitted"}
}

func frontmatter(data []byte) (map[string]any, *yaml.Node, string, error) {
	fm, body, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(data))
	if err != nil {
		return nil, nil, "", err
	}
	if fm == nil {
		return map[string]any{}, &yaml.Node{Kind: yaml.MappingNode}, body, nil
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	closeAt := strings.Index(normalized[4:], "\n---")
	header := ""
	if closeAt < 0 {
		if strings.HasPrefix(normalized[4:], "---") {
			header = ""
		} else {
			return nil, nil, "", fmt.Errorf("invalid delimiter")
		}
	} else {
		header = normalized[4 : 4+closeAt]
	}
	var document yaml.Node
	if header == "" {
		return fm, &yaml.Node{Kind: yaml.MappingNode}, body, nil
	}
	if err := yaml.Unmarshal([]byte(header), &document); err != nil {
		return nil, nil, "", err
	}
	if len(document.Content) == 0 {
		return fm, &yaml.Node{Kind: yaml.MappingNode}, body, nil
	}
	var hasMerge func(*yaml.Node) bool
	hasMerge = func(node *yaml.Node) bool {
		if node.Tag == "!!merge" {
			return true
		}
		for _, child := range node.Content {
			if hasMerge(child) {
				return true
			}
		}
		return false
	}
	if hasMerge(&document) {
		return nil, nil, "", fmt.Errorf("YAML merge fields are unsupported")
	}
	return fm, document.Content[0], body, nil
}

func loadMarkdown(src fs.FS, file string, kind ir.ResourceKind) (ir.Resource, error) {
	data, err := fs.ReadFile(src, file)
	if err != nil {
		return nil, err
	}
	fm, node, body, err := frontmatter(data)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(path.Base(file), ".md")
	if kind == ir.KindSkill {
		name = path.Base(path.Dir(file))
	}
	if value, ok := fm["name"]; ok {
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("invalid name")
		}
		name = s
	}
	if err := ir.ValidateName(name); err != nil {
		return nil, err
	}
	description := ""
	if value, ok := fm["description"]; ok {
		description, ok = value.(string)
		if !ok {
			return nil, fmt.Errorf("invalid description")
		}
	}
	allowed := []string{"name", "description"}
	if kind == ir.KindSkill {
		allowed = append(allowed, "license", "compatibility", "metadata", "allowed-tools", "permissions", "model", "triggers", "agent", "subagent", "argument-hint")
	}
	if kind == ir.KindCommand {
		allowed = append(allowed, "agent", "model", "subtask")
	}
	if kind == ir.KindSubagent {
		allowed = append(allowed, "mode", "model", "permission", "tools", "hidden", "temperature", "steps")
	}
	if err := strictKeys(fm, allowed); err != nil {
		return nil, err
	}
	if kind == ir.KindSkill {
		return bundle.LoadSkill(src, path.Dir(file))
	}
	native := &ext.OpenCode{}
	for _, key := range []string{"model", "agent", "mode"} {
		if value, ok := fm[key]; ok {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid string")
			}
			switch key {
			case "model":
				native.Model = s
			case "agent":
				native.Agent = s
			case "mode":
				native.Mode = s
			}
		}
	}
	if kind == ir.KindCommand {
		if value, ok := fm["subtask"]; ok {
			boolean, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("invalid subtask")
			}
			native.Subtask = &boolean
		}
		c := ir.NewCommand(name, description, body, nil, ir.SourceRef{File: file, Line: 1})
		c.Ext.OpenCode = native
		return c, nil
	}
	if native.Mode != "" && native.Mode != "subagent" && native.Mode != "primary" && native.Mode != "all" {
		return nil, fmt.Errorf("invalid mode")
	}
	if value, ok := fm["hidden"]; ok {
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("invalid hidden")
		}
		native.Hidden = &boolean
	}
	if value, ok := fm["steps"]; ok {
		integer, ok := value.(int)
		if !ok || integer < 1 {
			return nil, fmt.Errorf("invalid steps")
		}
		native.Steps = &integer
	}
	if value, ok := fm["temperature"]; ok {
		var temperature float64
		switch v := value.(type) {
		case float64:
			temperature = v
		case int:
			temperature = float64(v)
		default:
			return nil, fmt.Errorf("invalid temperature")
		}
		native.Temperature = &temperature
	}
	if value, ok := fm["tools"]; ok {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid tools")
		}
		native.ToolsSet = true
		native.Tools = map[string]bool{}
		for tool, v := range object {
			enabled, ok := v.(bool)
			if !ok || !nativeTool(tool) {
				return nil, fmt.Errorf("invalid tool")
			}
			native.Tools[tool] = enabled
		}
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "permission" {
			native.Permission, err = parsePermission(node.Content[i+1])
			if err != nil {
				return nil, err
			}
		}
	}
	// The native selection keeps its OpenCode spelling with source provenance;
	// the shipped model catalogs translate it for other targets.
	a := ir.NewSubagent(name, description, body, native.Model, nil, "", nil, "", nil, ir.SourceRef{File: file, Line: 1})
	a.ModelSource = string(target.TargetOpenCode)
	native.Model = ""
	a.Ext.OpenCode = native
	return a, nil
}

func strictKeys[V any](fields map[string]V, allowed []string) error {
	for key := range fields {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown or ambiguous modeled field")
		}
	}
	return nil
}

func nativeTool(tool string) bool {
	if strings.HasPrefix(tool, "mcp_") {
		return true
	}
	for _, known := range []string{"*", "bash", "read", "write", "edit", "glob", "grep", "webfetch", "websearch", "task", "todowrite", "todoread", "skill", "question", "lsp", "patch", "list", "external_directory", "doom_loop"} {
		if tool == known {
			return true
		}
	}
	return false
}

func parsePermission(node *yaml.Node) (*ext.OpenCodePermission, error) {
	valid := func(n *yaml.Node) bool {
		return n.Kind == yaml.ScalarNode && n.Tag == "!!str" && (n.Value == "allow" || n.Value == "deny" || n.Value == "ask")
	}
	if valid(node) {
		return &ext.OpenCodePermission{Action: node.Value}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("invalid permission")
	}
	p := &ext.OpenCodePermission{}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] || !nativeTool(key.Value) {
			return nil, fmt.Errorf("invalid permission key")
		}
		seen[key.Value] = true
		rule := ext.OpenCodePermissionRule{Tool: key.Value}
		if valid(value) {
			rule.Action = value.Value
		} else if value.Kind == yaml.MappingNode {
			rule.PatternsSet = true
			rule.Patterns = []ext.OpenCodePermissionPattern{}
			patterns := map[string]bool{}
			for j := 0; j < len(value.Content); j += 2 {
				pattern, action := value.Content[j], value.Content[j+1]
				if pattern.Kind != yaml.ScalarNode || pattern.Tag != "!!str" || patterns[pattern.Value] || !valid(action) {
					return nil, fmt.Errorf("invalid permission pattern")
				}
				patterns[pattern.Value] = true
				rule.Patterns = append(rule.Patterns, ext.OpenCodePermissionPattern{Pattern: pattern.Value, Action: action.Value})
			}
		} else {
			return nil, fmt.Errorf("invalid permission action")
		}
		p.Rules = append(p.Rules, rule)
	}
	return p, nil
}

func loadMCP(file, name string, node *jsonc.Node) (*ir.McpServer, error) {
	if err := ir.ValidateName(name); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if node.Kind() != jsonc.KindObject {
		return nil, fmt.Errorf("invalid MCP server")
	}
	if err := jsonc.Unmarshal(file, node.Canonical(), &fields); err != nil {
		return nil, err
	}
	var kind string
	if err := json.Unmarshal(fields["type"], &kind); err != nil {
		return nil, err
	}
	allowed := []string{"type", "enabled", "timeout"}
	switch kind {
	case "local":
		allowed = append(allowed, "command", "environment")
	case "remote":
		allowed = append(allowed, "url", "headers", "oauth")
	default:
		return nil, fmt.Errorf("invalid type")
	}
	if err := strictKeys(fields, allowed); err != nil {
		return nil, err
	}
	native := &ext.OpenCodeMCP{NativeValues: true}
	server := ir.NewMcpServer(name, "", nil, nil, nil, ir.SourceRef{File: file, Line: 1})
	server.Ext.OpenCode = &ext.OpenCode{MCP: native}
	if kind == "local" {
		var argv []string
		if err := json.Unmarshal(fields["command"], &argv); err != nil || len(argv) == 0 || argv[0] == "" {
			return nil, fmt.Errorf("invalid command")
		}
		server.Command, server.Args = argv[0], argv[1:]
		if raw, ok := fields["environment"]; ok {
			if err := stringDictionary(raw, &server.Env); err != nil {
				return nil, fmt.Errorf("invalid environment")
			}
		}
	} else {
		if err := json.Unmarshal(fields["url"], &server.URL); err != nil || server.URL == "" {
			return nil, fmt.Errorf("invalid URL")
		}
		if raw, ok := fields["headers"]; ok {
			if err := stringDictionary(raw, &server.Headers); err != nil {
				return nil, fmt.Errorf("invalid headers")
			}
		}
		if raw, ok := fields["oauth"]; ok {
			if string(raw) == "false" {
				disabled := true
				native.OAuthDisabled = &disabled
			} else {
				var oauth map[string]string
				if err := stringDictionary(raw, &oauth); err != nil {
					return nil, fmt.Errorf("invalid oauth")
				}
				if err := strictKeys(oauth, []string{"clientId", "clientSecret", "scope"}); err != nil {
					return nil, err
				}
				server.OAuthClientID, server.OAuthClientSecret, native.OAuthScope = oauth["clientId"], oauth["clientSecret"], oauth["scope"]
			}
		}
	}
	if raw, ok := fields["enabled"]; ok {
		var enabled bool
		if string(raw) == "null" {
			return nil, fmt.Errorf("invalid enabled")
		}
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, err
		}
		server.DisabledSet, server.Disabled = true, !enabled
	}
	if raw, ok := fields["timeout"]; ok {
		var timeout int
		if err := json.Unmarshal(raw, &timeout); err != nil || timeout < 1 {
			return nil, fmt.Errorf("invalid timeout")
		}
		native.Timeout = &timeout
	}
	return server, nil
}

var _ io.Reader = (*boundedFile)(nil)

func stringDictionary(raw []byte, out *map[string]string) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return fmt.Errorf("invalid dictionary")
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		var text string
		if string(value) == "null" {
			return fmt.Errorf("invalid dictionary value")
		}
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		result[key] = text
	}
	*out = result
	return nil
}
