package ingest_test

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/claude"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/codex"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/cursor"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/devin"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/windsurf"
)

func TestLoadBundleReportsUnconsumedWellKnownConfig(t *testing.T) {
	nativeMCP := []byte(`{"mcpServers":{"native":{"command":"npx"}}}`)
	foreignMCP := []byte(`{"mcpServers":{"ignored":{"command":"npx"}}}`)
	tests := []struct {
		name    string
		harness ingest.Harness
		src     fstest.MapFS
		layout  string
		code    string
		file    string
	}{
		{name: "claude", harness: ingest.HarnessClaude, src: fstest.MapFS{
			".claude-plugin/plugin.json": {Data: []byte(`{"name":"claude"}`)}, ".mcp.json": {Data: nativeMCP}, "mcp.json": {Data: foreignMCP},
		}, layout: "claude plugin", code: diag.CodeIngestClaudeUnconsumedConfig, file: "mcp.json"},
		{name: "factory", harness: ingest.HarnessFactory, src: fstest.MapFS{
			".factory-plugin/plugin.json": {Data: []byte(`{"name":"factory"}`)}, "mcp.json": {Data: nativeMCP}, ".mcp.json": {Data: foreignMCP},
		}, layout: "factory plugin", code: diag.CodeIngestFactoryUnconsumedConfig},
		{name: "cursor", harness: ingest.HarnessCursor, src: fstest.MapFS{
			".cursor-plugin/plugin.json": {Data: []byte(`{"name":"cursor"}`)}, "mcp.json": {Data: nativeMCP}, ".mcp.json": {Data: foreignMCP},
		}, layout: "cursor plugin", code: diag.CodeIngestCursorUnconsumedConfig},
		{name: "gemini", harness: ingest.HarnessGemini, src: fstest.MapFS{
			"gemini-extension.json": {Data: []byte(`{"name":"gemini","mcpServers":{"native":{"command":"npx"}}}`)}, ".gemini/settings.json": {Data: []byte(`{"hooks":{}}`)}, ".mcp.json": {Data: foreignMCP},
		}, layout: "gemini plugin", code: diag.CodeIngestGeminiUnconsumedConfig},
		{name: "windsurf", harness: ingest.HarnessWindsurf, src: fstest.MapFS{
			".windsurf/mcp_config.json": {Data: nativeMCP}, ".mcp.json": {Data: foreignMCP},
		}, layout: "windsurf workspace", code: diag.CodeIngestWindsurfUnconsumedConfig},
		{name: "devin", harness: ingest.HarnessDevin, src: fstest.MapFS{
			".devin/mcp_config.json": {Data: nativeMCP}, ".mcp.json": {Data: foreignMCP},
		}, layout: "devin workspace", code: diag.CodeIngestDevinUnconsumedConfig},
		{name: "codex", harness: ingest.HarnessCodex, src: fstest.MapFS{
			".codex/config.toml": {Data: []byte("[mcp_servers.native]\ncommand = \"npx\"\n")}, ".mcp.json": {Data: foreignMCP},
		}, layout: "codex workspace", code: diag.CodeIngestCodexUnconsumedConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing, ok := ingest.For(tt.harness)
			if !ok {
				t.Fatalf("ingester %q is not registered", tt.harness)
			}
			bundle, err := ing.LoadBundle(tt.src)
			if err != nil {
				t.Fatalf("LoadBundle: %v", err)
			}
			native, ignored := false, false
			for _, resource := range bundle.Resources {
				if resource.Kind() == ir.KindMcpServer && resource.Name() == "native" {
					native = true
				}
				if resource.Name() == "ignored" {
					ignored = true
				}
			}
			if !native || ignored {
				t.Fatalf("resources = %#v, native=%v ignored=%v", bundle.Resources, native, ignored)
			}
			var found *diag.Diagnostic
			if tt.file == "" {
				tt.file = ".mcp.json"
			}
			for i := range bundle.Diagnostics {
				if bundle.Diagnostics[i].Code == tt.code && bundle.Diagnostics[i].Source.File == tt.file {
					found = &bundle.Diagnostics[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("missing %s diagnostic: %#v", tt.code, bundle.Diagnostics)
			}
			if found.Severity != diag.SeverityWarning || found.Resource != "" || !strings.Contains(found.Message, tt.layout) || found.Hint == "" {
				t.Fatalf("diagnostic = %#v", *found)
			}
		})
	}
}

func TestFactoryMixedLayoutAuditsWorkspaceFiles(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"factory"}`)},
		"hooks/hooks.json":            {Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo root"}]}}`)},
		"mcp.json":                    {Data: []byte(`{"mcpServers":{"root":{"command":"npx"}}}`)},
		".factory/hooks/hooks.json":   {Data: []byte(`{"hooks":{"PostToolUse":[{"type":"command","command":"echo workspace"}]}}`)},
		".factory/mcp.json":           {Data: []byte(`{"mcpServers":{"workspace":{"command":"npx"}}}`)},
	}
	ing, _ := ingest.For(ingest.HarnessFactory)
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(bundle.Resources, ir.KindHook, "PreToolUse-command") || !hasResource(bundle.Resources, ir.KindMcpServer, "root") {
		t.Fatalf("plugin resources missing: %#v", bundle.Resources)
	}
	var files []string
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Code != diag.CodeIngestFactoryUnconsumedConfig {
			continue
		}
		files = append(files, diagnostic.Source.File)
	}
	if !slices.Equal(files, []string{".factory/mcp.json", ".factory/hooks/hooks.json"}) {
		t.Fatalf("workspace warnings = %v", files)
	}
}

func TestClaudeWorkspaceAuditsForeignRootHooks(t *testing.T) {
	src := fstest.MapFS{
		".claude/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		".claude/hooks/hooks.json":     {Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo workspace"}]}}`)},
		"hooks/hooks.json":             {Data: []byte(`{"hooks":{"PostToolUse":[{"type":"command","command":"echo root"}]}}`)},
	}
	ing, _ := ingest.For(ingest.HarnessClaude)
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(bundle.Resources, ir.KindHook, "PreToolUse-command") {
		t.Fatalf("workspace hook missing: %#v", bundle.Resources)
	}
	if !hasDiagnostic(bundle.Diagnostics, diag.CodeIngestClaudeUnconsumedConfig, "hooks/hooks.json") {
		t.Fatalf("missing root hook warning: %#v", bundle.Diagnostics)
	}
}

func TestFactoryMalformedConsumedMCPIsLoadFailureOnly(t *testing.T) {
	src := fstest.MapFS{
		".factory-plugin/plugin.json": {Data: []byte(`{"name":"factory"}`)},
		"skills/demo/SKILL.md":        {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		"mcp.json":                    {Data: []byte(`{"mcpServers":{"NOT-valid":{"command":"npx"}}}`)},
	}
	ing, _ := ingest.For(ingest.HarnessFactory)
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(bundle.Resources, ir.KindSkill, "demo") {
		t.Fatalf("skill was lost: %#v", bundle.Resources)
	}
	if !hasDiagnostic(bundle.Diagnostics, diag.CodeIngestFactoryLoadMcpServersFailed, "mcp.json") {
		t.Fatalf("missing MCP load failure: %#v", bundle.Diagnostics)
	}
	if hasDiagnostic(bundle.Diagnostics, diag.CodeIngestFactoryUnconsumedConfig, "mcp.json") {
		t.Fatalf("malformed consumed MCP was audited as unconsumed: %#v", bundle.Diagnostics)
	}
}

func TestUnconsumedDiagnosticsHaveDeterministicInventoryOrder(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		".mcp.json":            {Data: []byte(`{"mcpServers":{}}`)},
		"mcp.json":             {Data: []byte(`{"mcpServers":{}}`)},
		"hooks/hooks.json":     {Data: []byte(`{"hooks":{}}`)},
		".claude.json":         {Data: []byte(`{"mcpServers":{}}`)},
		"README.md":            {Data: []byte("unrelated")},
	}
	before := unconsumedFiles(t, src)
	after := unconsumedFiles(t, src)
	want := []string{"mcp.json", "hooks/hooks.json", ".claude.json"}
	if !slices.Equal(before, want) || !slices.Equal(after, want) || !slices.Equal(before, after) {
		t.Fatalf("warning order = %v then %v, want %v", before, after, want)
	}
}

func TestClaudeBareFallbackPreservesInputsAndResourceIdentity(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		"commands/hi.md":       {Data: []byte("---\nname: hi\ndescription: Hi\n---\nHi")},
		"mcp.json":             {Data: []byte(`{"mcpServers":{"probe":{"command":"npx","args":["-y","x"]}}}`)},
		"README.md":            {Data: []byte("unrelated")},
	}
	before := append([]byte(nil), src["mcp.json"].Data...)
	ing, _ := ingest.For(ingest.HarnessClaude)
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(bundle.Resources, ir.KindSkill, "demo") || !hasResource(bundle.Resources, ir.KindCommand, "hi") {
		t.Fatalf("sibling resources missing: %#v", bundle.Resources)
	}
	var server *ir.McpServer
	for _, resource := range bundle.Resources {
		if resource.Kind() == ir.KindMcpServer {
			server = resource.(*ir.McpServer)
		}
	}
	if server == nil || server.Name() != "probe" || server.Command != "npx" || !slices.Equal(server.Args, []string{"-y", "x"}) || server.Source.File != "mcp.json" {
		t.Fatalf("bare MCP = %#v", server)
	}
	if !slices.Equal(before, src["mcp.json"].Data) {
		t.Fatal("MapFS input bytes changed")
	}
}

func TestClaudeDottedMCPPrecedence(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		".mcp.json":            {Data: []byte(`{"mcpServers":{"dotted":{"command":"npx"}}}`)},
		"mcp.json":             {Data: []byte(`{"mcpServers":{"undotted":{"command":"npx"}}}`)},
	}
	ing, _ := ingest.For(ingest.HarnessClaude)
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	var servers []string
	for _, resource := range bundle.Resources {
		if resource.Kind() == ir.KindMcpServer {
			servers = append(servers, resource.Name())
		}
	}
	if !slices.Equal(servers, []string{"dotted"}) || !hasDiagnostic(bundle.Diagnostics, diag.CodeIngestClaudeUnconsumedConfig, "mcp.json") {
		t.Fatalf("servers=%v diagnostics=%#v", servers, bundle.Diagnostics)
	}
}

type permissionFS struct{ fs.FS }

func (p permissionFS) Open(name string) (fs.File, error) {
	if name == ".mcp.json" {
		return nil, fs.ErrPermission
	}
	return p.FS.Open(name)
}

func TestClaudeBareMCPStatPermissionDoesNotFallback(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		".mcp.json":            {Data: []byte(`{"mcpServers":{"dotted":{"command":"npx"}}}`)},
		"mcp.json":             {Data: []byte(`{"mcpServers":{"undotted":{"command":"npx"}}}`)},
	}
	ing, _ := ingest.For(ingest.HarnessClaude)
	_, err := ing.LoadBundle(permissionFS{FS: src})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("LoadBundle error = %v, want permission error", err)
	}
}

func TestLoadBundleWarnsMalformedForeignConfigWithoutReadingIt(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"claude"}`)},
		".mcp.json":                  {Data: []byte(`{"mcpServers":{}}`)},
		"mcp.json":                   {Data: []byte("{")},
	}
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester is not registered")
	}
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if !hasDiagnostic(bundle.Diagnostics, diag.CodeIngestClaudeUnconsumedConfig, "mcp.json") {
		t.Fatalf("missing malformed foreign config warning: %#v", bundle.Diagnostics)
	}
}

func TestLoadBundleDoesNotWarnConsumedEmptyConfigOrDirectory(t *testing.T) {
	src := fstest.MapFS{
		"skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo")},
		".mcp.json":            {Data: []byte(`{"mcpServers":{}}`)},
		"mcp.json/marker":      {Data: []byte("directory")},
	}
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester is not registered")
	}
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Code == diag.CodeIngestClaudeUnconsumedConfig {
			t.Fatalf("unexpected unconsumed diagnostic: %#v", diagnostic)
		}
	}
}

func hasResource(resources []ir.Resource, kind ir.ResourceKind, name string) bool {
	for _, resource := range resources {
		if resource.Kind() == kind && resource.Name() == name {
			return true
		}
	}
	return false
}

func hasDiagnostic(diagnostics []diag.Diagnostic, code, file string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Source.File == file {
			return true
		}
	}
	return false
}

func unconsumedFiles(t *testing.T, src fstest.MapFS) []string {
	t.Helper()
	ing, ok := ingest.For(ingest.HarnessClaude)
	if !ok {
		t.Fatal("Claude ingester is not registered")
	}
	bundle, err := ing.LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Code == diag.CodeIngestClaudeUnconsumedConfig {
			files = append(files, diagnostic.Source.File)
		}
	}
	return files
}
