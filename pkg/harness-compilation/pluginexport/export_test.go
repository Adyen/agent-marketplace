package pluginexport

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

func TestExportBuildsAgentPluginAndReimportsEquivalently(t *testing.T) {
	// spec: plugin-portable-format/exported-agent-plugins-package-re-imports-equivalently
	skillText := "---\nname: review\ndescription: Review code\nlicense: MIT\ncompatibility: Go projects\nmetadata:\n  team: platform\nallowed-tools: [Read, Edit]\n---\nReview the change.\n"
	files := fstest.MapFS{
		".claude-plugin/plugin.json":   &fstest.MapFile{Data: []byte(`{"name":"review-kit","description":"Code review helpers","version":"1.2.3","license":"Apache-2.0","keywords":["review","code"]}`)},
		"skills/review/SKILL.md":       &fstest.MapFile{Data: []byte(skillText)},
		"skills/review/scripts/run.sh": &fstest.MapFile{Data: []byte("#!/bin/sh\n"), Mode: 0o755},
		".mcp.json":                    &fstest.MapFile{Data: []byte(`{"mcpServers":{"local-index":{"command":"node"}}}`)},
	}
	skill := ir.NewSkillWithOptions("review", "Review code", "Review the change.\n", nil,
		map[string]any{"license": "MIT", "compatibility": "Go projects", "metadata": map[string]any{"team": "platform"}},
		ir.SourceRef{File: "skills/review/SKILL.md"},
		ir.WithSkillAllowedTools([]string{"Read", "Edit"}))
	server := ir.NewMcpServer("local-index", bundle.CanonicalRootToken+"/bin/server", []string{bundle.CanonicalRootToken + "/config"}, map[string]string{"MODE": "safe"}, nil,
		ir.SourceRef{File: ".mcp.json"})
	server.Cwd = bundle.CanonicalRootToken + "/data"
	sse := ir.NewMcpServerWithOptions("event-stream", "", nil, nil, nil, ir.SourceRef{File: ".mcp.json"},
		ir.WithMcpURL("https://example.test/events"), ir.WithMcpTransport("sse"),
		ir.WithMcpHeaders(map[string]string{"Accept": "text/event-stream"}))
	http := ir.NewMcpServerWithOptions("stream", "", nil, nil, nil, ir.SourceRef{File: ".mcp.json"},
		ir.WithMcpURL("https://example.test/mcp"), ir.WithMcpTransport("streamable-http"))
	plugin := &ir.Plugin{
		Format: "claude-plugin",
		Manifest: ir.Manifest{
			Name:        "review-kit",
			Description: "Code review helpers",
			Version:     "1.2.3",
			Extra:       map[string]any{"license": "Apache-2.0", "keywords": []any{"review", "code"}},
		},
		Members: []ir.PluginMember{{Resource: skill}, {Resource: server}, {Resource: sse}, {Resource: http}},
		Files: []ir.PluginFile{
			{Path: ".claude-plugin/plugin.json"},
			{Path: "skills/review/SKILL.md"},
			{Path: "skills/review/scripts/run.sh", Executable: true},
			{Path: ".mcp.json"},
		},
	}

	result, err := Export(plugin, files, Options{})
	if err != nil {
		t.Fatalf("Export: %v; losses=%+v", err, result.Losses)
	}
	assertFilePartition(t, plugin, result,
		map[string]map[string]bool{
			".claude-plugin/plugin.json": {"plugin.json": true},
			".mcp.json":                  {"mcp.json": true},
		},
		map[string]string{
			"skills/review/SKILL.md":       "skills/review/SKILL.md",
			"skills/review/scripts/run.sh": "skills/review/scripts/run.sh",
		}, nil)
	support := resultFileEntry(t, result, "skills/review/scripts/run.sh")
	if !support.Executable || string(support.Bytes) != "#!/bin/sh\n" {
		t.Fatalf("support file metadata = %+v", support)
	}
	var manifest map[string]any
	if err := json.Unmarshal(resultFileEntry(t, result, "plugin.json").Bytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != pluginSchemaID || manifest["license"] != "Apache-2.0" ||
		!reflect.DeepEqual(manifest["keywords"], []any{"review", "code"}) {
		t.Fatalf("portable manifest fields = %#v", manifest)
	}
	root := t.TempDir()
	for _, file := range result.Files {
		mode := fs.FileMode(0o644)
		if file.Executable {
			mode = 0o755
		}
		name := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, file.Bytes, mode); err != nil {
			t.Fatal(err)
		}
	}
	imported, _, err := plugindecode.Decode(root, "agent-plugin")
	if err != nil {
		t.Fatalf("re-import exported package: %v", err)
	}
	if imported.Name() != plugin.Name() || imported.Manifest.Description != plugin.Manifest.Description ||
		imported.Manifest.Version != plugin.Manifest.Version || len(imported.Members) != len(plugin.Members) {
		t.Fatalf("re-imported manifest or members differ: %#v", imported)
	}
	var gotSkill *ir.Skill
	gotServers := map[string]*ir.McpServer{}
	for _, member := range imported.Members {
		switch resource := member.Resource.(type) {
		case *ir.Skill:
			gotSkill = resource
		case *ir.McpServer:
			gotServers[resource.Name()] = resource
		}
	}
	if gotSkill == nil || len(gotServers) != 3 {
		t.Fatalf("re-imported members have unexpected types: %#v", imported.Members)
	}
	if gotSkill.Name() != skill.Name() || gotSkill.Description != skill.Description || gotSkill.Body != skill.Body ||
		!reflect.DeepEqual(gotSkill.AllowedTools, skill.AllowedTools) || !reflect.DeepEqual(gotSkill.Extra, skill.Extra) {
		t.Fatalf("re-imported skill differs: %#v", gotSkill)
	}
	gotServer := gotServers[server.Name()]
	gotSSE := gotServers[sse.Name()]
	gotHTTP := gotServers[http.Name()]
	if gotServer == nil || gotSSE == nil || gotHTTP == nil {
		t.Fatalf("re-imported MCP names differ: %#v", gotServers)
	}
	if gotServer.Command != "./bin/server" || !reflect.DeepEqual(gotServer.Args, []string{"${PLUGIN_ROOT}/config"}) ||
		!reflect.DeepEqual(gotServer.Env, server.Env) || gotServer.Cwd != "${PLUGIN_ROOT}/data" ||
		gotSSE.Transport != sse.Transport || gotSSE.URL != sse.URL || !reflect.DeepEqual(gotSSE.Headers, sse.Headers) ||
		gotHTTP.Transport != http.Transport || gotHTTP.URL != http.URL {
		t.Fatalf("re-imported MCP servers differ: %#v", gotServers)
	}
	if text := string(resultFileEntry(t, result, "skills/review/SKILL.md").Bytes); !strings.Contains(text, "Review the change.") {
		t.Fatalf("skill content was not preserved: %q", text)
	}
}

func TestExportLossIsIntolerantAndAllowLossRewritesOnlyUnsupportedSkillFields(t *testing.T) {
	// spec: plugin-portable-format/export-with-a-loss-writes-nothing
	// spec: plugin-portable-format/allow-loss-export-omits-and-reports-non-security-members
	rawSkill := "---\nname: review\ndescription: Review code\nargument-hint: <path>\nlicense: MIT\n---\nKeep this body.\n"
	plugin := &ir.Plugin{
		Format:     "claude-plugin",
		Manifest:   ir.Manifest{Name: "review-kit", Extra: map[string]any{"unpublished": "value", "not-authored": "value"}},
		Extensions: map[string]any{"com.example.client": map[string]any{"flag": true}},
		Members: []ir.PluginMember{
			{Resource: ir.NewSkillWithOptions("review", "Review code", "Keep this body.\n", nil,
				map[string]any{"license": "MIT"}, ir.SourceRef{File: "skills/review/SKILL.md"},
				ir.WithSkillArgumentHint("<path>"))},
			{Resource: ir.NewHook("pre-check", "PreToolUse", "echo check", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})},
		},
		Files: []ir.PluginFile{{Path: ".claude-plugin/plugin.json"}, {Path: "skills/review/SKILL.md"}, {Path: "hooks/hooks.json"}},
	}
	source := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"review-kit","unpublished":"value"}`)},
		"skills/review/SKILL.md":     {Data: []byte(rawSkill)},
		"hooks/hooks.json":           {Data: []byte(`{"PreToolUse":[]}`)},
	}
	result, err := Export(plugin, source, Options{})
	if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportLoss {
		t.Fatalf("loss error = %v, want %s", err, diag.CodePluginExportLoss)
	}
	if len(result.Files) != 0 || !containsLoss(result.Losses, "hook/pre-check") ||
		!containsLoss(result.Losses, "argument-hint") || !containsLoss(result.Losses, "unpublished") ||
		containsLoss(result.Losses, "not-authored") {
		t.Fatalf("loss result must identify omissions without output files: %+v", result)
	}
	invalid := &ir.Plugin{Format: "claude-plugin", Manifest: ir.Manifest{Name: "Review_Kit"}}
	_, err = Export(invalid, source, Options{AllowLoss: true})
	if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportLoss {
		t.Fatalf("invalid output name did not refuse export: %v", err)
	}

	result, err = Export(plugin, source, Options{AllowLoss: true})
	if err != nil {
		t.Fatalf("allow-loss export: %v", err)
	}
	assertFilePartition(t, plugin, result, map[string]map[string]bool{
		".claude-plugin/plugin.json": {"plugin.json": true},
	},
		map[string]string{"skills/review/SKILL.md": "skills/review/SKILL.md"},
		map[string]string{"hooks/hooks.json": "hook/pre-check"})
	if !containsLoss(result.Losses, "hook/pre-check") {
		t.Fatalf("omitted hook was not reported: %+v", result.Losses)
	}
	for _, file := range result.Files {
		if file.Path == "hooks/hooks.json" {
			t.Fatal("hook definition was copied")
		}
		if file.Path == "skills/review/SKILL.md" {
			text := string(file.Bytes)
			if strings.Contains(text, "argument-hint") || !strings.Contains(text, "Keep this body.") ||
				!strings.Contains(text, "license: MIT") {
				t.Fatalf("lossy SKILL.md rewrite = %q", text)
			}
		}
	}
}

func TestExportCannotDropSecurityRestrictionsWithAllowLoss(t *testing.T) {
	// spec: plugin-portable-format/allow-loss-cannot-drop-a-restriction
	tests := []struct {
		name        string
		member      ir.Resource
		files       fstest.MapFS
		packageFile string
	}{
		{
			name:        "disabled MCP",
			packageFile: "mcp/servers.json",
			member: ir.NewMcpServerWithOptions("local", "node", nil, nil, nil,
				ir.SourceRef{File: "mcp/servers.json"}, ir.WithMcpDisabled(true)),
			files: fstest.MapFS{"mcp/servers.json": {Data: []byte("{}")}},
		},
		{
			name:        "skill permissions",
			packageFile: "skills/limited/SKILL.md",
			member: ir.NewSkillWithOptions("limited", "", "Body\n", nil, nil, ir.SourceRef{File: "skills/limited/SKILL.md"},
				ir.WithSkillPermissions(ir.Permissions{Allow: []string{"Read"}})),
			files: fstest.MapFS{"skills/limited/SKILL.md": {Data: []byte("---\nname: limited\npermissions:\n  allow: [Read]\n---\nBody\n")}},
		},
		{
			name:        "model-excluding triggers",
			packageFile: "skills/manual/SKILL.md",
			member:      ir.NewSkill("manual", "", "Body\n", []string{"user"}, nil, ir.SourceRef{File: "skills/manual/SKILL.md"}),
			files:       fstest.MapFS{"skills/manual/SKILL.md": {Data: []byte("---\nname: manual\ntriggers: [user]\n---\nBody\n")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &ir.Plugin{
				Format:   "claude-plugin",
				Manifest: ir.Manifest{Name: "secure-kit"},
				Members:  []ir.PluginMember{{Resource: tt.member}},
				Files:    []ir.PluginFile{{Path: tt.packageFile}},
			}
			result, err := Export(plugin, tt.files, Options{AllowLoss: true})
			if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportRestriction {
				t.Fatalf("restriction result=%+v error=%v, want %s", result, err, diag.CodePluginExportRestriction)
			}
			if len(result.Files) != 0 {
				t.Fatalf("restriction refusal returned writable files: %+v", result.Files)
			}
		})
	}
}

func TestExportMCPMappingAndSourceInterpolation(t *testing.T) {
	tests := []struct {
		name, field, value, wantType string
		allowLoss, wantLoss          bool
	}{
		{"command ${NODE_BIN}", "command", "${NODE_BIN}", "", true, true},
		{"args", "args", "${TOKEN}", "", true, true},
		{"env", "env", "${env:AUTH_TOKEN}", "", true, true},
		{"native plugin-root args", "args", "${PLUGIN_ROOT}/cfg.json", "", true, true},
		{"native plugin-data env", "env", "${PLUGIN_DATA}/secrets", "", true, true},
		{"cwd", "cwd", "${file:config/workdir}", "", true, true},
		{"headers", "headers", "${TOKEN}", "", true, true},
		{"url", "url", "https://${HOST}/mcp", "", true, true},
		{"native stdio type", "type", "stdio", "stdio", false, false},
		{"native http type", "type", "http", "streamable-http", false, false},
		{"unknown native type", "type", "custom", "", false, true},
		{"mismatched native type", "type", "sse", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := interpolationServer()
			switch tt.field {
			case "command":
				server.Command = tt.value
			case "args":
				server.Args = []string{tt.value}
			case "env":
				server.Env = map[string]string{"TOKEN": tt.value}
			case "cwd":
				server.Cwd = tt.value
			case "headers":
				server.Command, server.URL, server.Transport = "", "https://example.test/mcp", "sse"
				server.Headers = map[string]string{"Authorization": tt.value}
			case "url":
				server.Command, server.URL, server.Transport = "", tt.value, "streamable-http"
			case "type":
				server.Extra = map[string]any{"type": tt.value}
				if tt.value == "http" {
					server.Command, server.URL = "", "https://example.test/mcp"
				}
			}
			result, err := Export(interpolationPlugin("claude-plugin", server), interpolationFiles(), Options{AllowLoss: tt.allowLoss})
			if tt.wantLoss {
				if tt.allowLoss {
					if err != nil || !containsLoss(result.Losses, "mcpServers/interpolated") || hasFile(result, "mcp.json") {
						t.Fatalf("unrepresentable member was not omitted: result=%+v error=%v", result, err)
					}
				} else if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportLoss {
					t.Fatalf("invalid native type error = %v", err)
				}
				return
			}
			if err != nil || len(result.Losses) != 0 {
				t.Fatalf("MCP mapping %s was lossy: result=%+v error=%v", tt.name, result, err)
			}
			var config struct {
				MCPServers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(resultFileEntry(t, result, "mcp.json").Bytes, &config); err != nil {
				t.Fatal(err)
			}
			if config.MCPServers["interpolated"]["type"] != tt.wantType {
				t.Fatalf("exported MCP type = %#v, want %q", config.MCPServers, tt.wantType)
			}
		})
	}
}

func TestExportAccountsForStrayMCPFiles(t *testing.T) {
	server := interpolationServer()
	claude := interpolationPlugin("claude-plugin", server)
	claude.Files = append(claude.Files, ir.PluginFile{Path: "mcp.json"})
	nonstandardServer := interpolationServer()
	nonstandardServer.Source.File = "mcp-extra.json"
	nonstandard := interpolationPlugin("claude-plugin", nonstandardServer)
	nonstandard.Files = []ir.PluginFile{{Path: ".claude-plugin/plugin.json"}, {Path: "mcp-extra.json"}, {Path: "mcp.json"}}
	nonstandardFiles := interpolationFiles()
	nonstandardFiles["mcp-extra.json"] = &fstest.MapFile{Data: []byte(`{"source":"member"}`)}
	tests := []struct {
		name, stray, member string
		plugin              *ir.Plugin
		files               fstest.MapFS
		consumed            map[string]string
	}{
		{"claude twin configs", "mcp.json", "interpolated", claude, interpolationFiles(), map[string]string{".claude-plugin/plugin.json": "plugin.json", ".mcp.json": "mcp.json"}},
		{"agent-only stray", ".mcp.json", "", &ir.Plugin{Format: "agent-plugin", Manifest: ir.Manifest{Name: "portable-kit"}, Files: []ir.PluginFile{{Path: ".mcp.json"}}}, fstest.MapFS{".mcp.json": {Data: []byte(`{"mcpServers":{}}`)}}, nil},
		{"nonstandard source file", "mcp.json", "interpolated", nonstandard, nonstandardFiles, map[string]string{".claude-plugin/plugin.json": "plugin.json", "mcp-extra.json": "mcp.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Export(tt.plugin, tt.files, Options{})
			if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportLoss || !containsLoss(result.Losses, tt.stray) {
				t.Fatalf("stray MCP file was not refused: result=%+v error=%v", result, err)
			}
			result, err = Export(tt.plugin, tt.files, Options{AllowLoss: true})
			if err != nil || !containsLoss(result.Losses, tt.stray) {
				t.Fatalf("allow-loss did not report the stray: result=%+v error=%v", result, err)
			}
			if tt.member == "" {
				if hasFile(result, "mcp.json") || hasFile(result, ".mcp.json") {
					t.Fatalf("stray-only package emitted MCP configuration: %+v", result.Files)
				}
			} else {
				var config struct {
					MCPServers map[string]map[string]any `json:"mcpServers"`
				}
				if err := json.Unmarshal(resultFileEntry(t, result, "mcp.json").Bytes, &config); err != nil {
					t.Fatal(err)
				}
				if config.MCPServers[tt.member]["command"] != "node" {
					t.Fatalf("generated MCP configuration differs: %#v", config.MCPServers)
				}
			}
			consumed := map[string]map[string]bool{}
			for sourcePath, artifact := range tt.consumed {
				consumed[sourcePath] = map[string]bool{artifact: true}
			}
			assertFilePartition(t, tt.plugin, result, consumed, nil, map[string]string{tt.stray: tt.stray})
		})
	}
}

func TestExportReportsRenamedSkillOutputCollision(t *testing.T) {
	plugin := &ir.Plugin{
		Format: "claude-plugin", Manifest: ir.Manifest{Name: "portable-kit"},
		Members: []ir.PluginMember{{Resource: ir.NewSkill("renamed", "", "Body\n", nil, nil, ir.SourceRef{File: "skills/source/SKILL.md"})}},
		Files: []ir.PluginFile{
			{Path: ".claude-plugin/plugin.json"}, {Path: "skills/source/SKILL.md"},
			{Path: "skills/source/asset.txt"}, {Path: "skills/renamed/asset.txt"},
		},
	}
	source := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"portable-kit"}`)},
		"skills/source/SKILL.md":     {Data: []byte("---\nname: renamed\n---\nBody\n")},
		"skills/source/asset.txt":    {Data: []byte("member")},
		"skills/renamed/asset.txt":   {Data: []byte("literal")},
	}
	result, err := Export(plugin, source, Options{})
	if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodePluginExportLoss ||
		!containsLoss(result.Losses, "skills/source/asset.txt") {
		t.Fatalf("renamed skill collision was not refused: result=%+v error=%v", result, err)
	}
	result, err = Export(plugin, source, Options{AllowLoss: true})
	if err != nil {
		t.Fatalf("allow-loss collision export: %v", err)
	}
	assertFilePartition(t, plugin, result, map[string]map[string]bool{
		".claude-plugin/plugin.json": {"plugin.json": true},
	},
		map[string]string{
			"skills/source/SKILL.md":   "skills/renamed/SKILL.md",
			"skills/renamed/asset.txt": "skills/renamed/asset.txt",
		}, map[string]string{"skills/source/asset.txt": "skills/source/asset.txt"})
}

func TestExportKeepsCanonicalInterpolationLiteralForAgentPluginSources(t *testing.T) {
	stdio := interpolationServer()
	stdio.Source.File = "mcp.json"
	stdio.Args = []string{"${TOKEN}"}
	stdio.Env = map[string]string{"TOKEN": "${env:AUTH_TOKEN}"}
	stdio.Cwd = "${PLUGIN_DATA}"
	remote := interpolationServer()
	remote.Source.File = "mcp.json"
	remote.SetName("remote")
	remote.Command = ""
	remote.URL = "https://${HOST}/mcp"
	remote.Transport = "streamable-http"
	remote.Headers = map[string]string{"Authorization": "${file:token}"}
	result, err := Export(
		interpolationPlugin("agent-plugin", stdio, remote),
		interpolationFiles(),
		Options{},
	)
	if err != nil || len(result.Losses) != 0 {
		t.Fatalf("Agent Plugins interpolation was not preserved literally: result=%+v error=%v", result, err)
	}
	if !hasFile(result, "mcp.json") {
		t.Fatalf("Agent Plugins MCP configuration is absent: %+v", result.Files)
	}
	server := ir.NewMcpServerWithOptions("default-enabled", "node", nil, nil, nil, ir.SourceRef{File: ".mcp.json"},
		ir.WithMcpDisabled(false))
	result, err = Export(interpolationPlugin("claude-plugin", server), interpolationFiles(), Options{})
	if err != nil || len(result.Losses) != 0 || !hasFile(result, "mcp.json") {
		t.Fatalf("explicit disabled=false changed the export: result=%+v error=%v", result, err)
	}
}

func interpolationServer() *ir.McpServer {
	return ir.NewMcpServer("interpolated", "node", nil, nil, nil, ir.SourceRef{File: ".mcp.json"})
}

func interpolationPlugin(format string, servers ...*ir.McpServer) *ir.Plugin {
	members := make([]ir.PluginMember, len(servers))
	for i, server := range servers {
		members[i] = ir.PluginMember{Resource: server}
	}
	paths := []string{".claude-plugin/plugin.json", ".mcp.json"}
	if format == "agent-plugin" {
		paths = []string{"plugin.json", "mcp.json"}
	}
	inventory := make([]ir.PluginFile, len(paths))
	for i, file := range paths {
		inventory[i] = ir.PluginFile{Path: file}
	}
	return &ir.Plugin{Format: format, Manifest: ir.Manifest{Name: "portable-kit"}, Members: members, Files: inventory}
}

func interpolationFiles() fstest.MapFS {
	return fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"portable-kit"}`)},
		".mcp.json":                  {Data: []byte(`{"mcpServers":{}}`)},
		"plugin.json":                {Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"portable-kit"}`)},
		"mcp.json":                   {Data: []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{}}`)},
	}
}

func hasFile(result Result, name string) bool {
	for _, file := range result.Files {
		if file.Path == name {
			return true
		}
	}
	return false
}

func assertFilePartition(t *testing.T, plugin *ir.Plugin, result Result, consumed map[string]map[string]bool, emitted, lost map[string]string) {
	t.Helper()
	outputs := map[string]bool{}
	for _, file := range result.Files {
		if outputs[file.Path] {
			t.Fatalf("duplicate output path %q", file.Path)
		}
		outputs[file.Path] = true
	}
	for _, file := range plugin.Files {
		categories := 0
		if artifacts := consumed[file.Path]; len(artifacts) > 0 {
			categories++
			for artifact := range artifacts {
				if !outputs[artifact] {
					t.Errorf("input %q consumed into missing artifact %q", file.Path, artifact)
				}
				if file.Path != artifact && outputs[file.Path] {
					t.Errorf("consumed input %q was also emitted as a file", file.Path)
				}
			}
		}
		if output, ok := emitted[file.Path]; ok {
			categories++
			if !outputs[output] {
				t.Errorf("input %q mapped to missing output %q", file.Path, output)
			}
		}
		if loss, ok := lost[file.Path]; ok {
			categories++
			if !containsLoss(result.Losses, loss) {
				t.Errorf("input %q lacks loss %q: %+v", file.Path, loss, result.Losses)
			}
		}
		if categories != 1 {
			t.Errorf("input %q has %d accounting categories", file.Path, categories)
		}
	}
}

func containsLoss(losses []Loss, fragment string) bool {
	for _, loss := range losses {
		if strings.Contains(loss.Path, fragment) || strings.Contains(loss.Reason, fragment) {
			return true
		}
	}
	return false
}

func resultFileEntry(t *testing.T, result Result, name string) File {
	t.Helper()
	for _, file := range result.Files {
		if file.Path == name {
			return file
		}
	}
	t.Fatalf("file %q absent from export", name)
	return File{}
}
