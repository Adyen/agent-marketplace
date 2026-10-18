package windsurf

import (
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var testWorkspaceFS = fstest.MapFS{
	".windsurf/skills/deploy/SKILL.md": &fstest.MapFile{
		Data: []byte("# Deploy\n\nUse the workflow.\n"),
	},
	".windsurf/workflows/format.md": &fstest.MapFile{
		Data: []byte("---\nname: format\ndescription: Format the project\nresource: command\n---\nmake fmt\n"),
	},
	".windsurf/hooks/pre_read_code.json": &fstest.MapFile{
		Data: []byte("{\n  \"command\": \"scripts/check-read.sh\",\n  \"event\": \"pre_read_code\",\n  \"type\": \"command\"\n}\n"),
	},
	".windsurf/hooks/post_setup_worktree.json": &fstest.MapFile{
		Data: []byte("{\n  \"command\": \"scripts/setup.sh\",\n  \"event\": \"post_setup_worktree\",\n  \"type\": \"command\",\n  \"powershell\": \"Write-Host setup\",\n  \"show_output\": false,\n  \"working_directory\": \"/tmp/project\"\n}\n"),
	},
	".windsurf/mcp_config.json": &fstest.MapFile{
		Data: []byte("{\n  \"mcpServers\": {\n    \"github\": {\n      \"args\": [\"-y\", \"@modelcontextprotocol/server-github\"],\n      \"command\": \"npx\",\n      \"env\": {\n        \"GITHUB_TOKEN\": \"${env:GITHUB_TOKEN}\",\n        \"SETTINGS\": \"./settings.json\"\n      }\n    }\n  }\n}\n"),
	},
	"AGENTS.md": &fstest.MapFile{
		Data: []byte("# ignored\n"),
	},
}

var testWorkspaceRootFS = fstest.MapFS{
	"skills/deploy/SKILL.md": &fstest.MapFile{
		Data: []byte("# Deploy\n\nUse the workflow.\n"),
	},
}

func TestIngesterDetectsWorkspaceLayout(t *testing.T) {
	ing := Ingester{}
	if !ing.Detect(testWorkspaceFS) {
		t.Fatal("Detect() = false, want true")
	}
}

func TestIngesterDetectsWorkspaceRootLayout(t *testing.T) {
	ing := Ingester{}
	if !ing.Detect(testWorkspaceRootFS) {
		t.Fatal("Detect() = false, want true")
	}
}

func TestLoadSkill(t *testing.T) {
	skill, err := LoadSkill(testWorkspaceFS, ".windsurf/skills/deploy")
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if skill.Name() != "deploy" {
		t.Fatalf("skill.Name() = %q, want %q", skill.Name(), "deploy")
	}
	if skill.Body != "# Deploy\n\nUse the workflow.\n" {
		t.Fatalf("skill.Body = %q", skill.Body)
	}
}

func TestLoadWorkflow_AssumedCommand(t *testing.T) {
	cmd, diags, err := LoadWorkflow(testWorkspaceFS, ".windsurf/workflows/format.md")
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	if cmd.Name() != "format" {
		t.Fatalf("cmd.Name() = %q, want %q", cmd.Name(), "format")
	}
	if cmd.Description != "Format the project" {
		t.Fatalf("cmd.Description = %q", cmd.Description)
	}
	if cmd.Body != "make fmt\n" {
		t.Fatalf("cmd.Body = %q", cmd.Body)
	}
	if len(diags) != 1 || diags[0].Code != diag.CodeIngestWindsurfWorkflowAssumedCommand {
		t.Fatalf("LoadWorkflow diags = %#v, want ingest.windsurf.workflow-assumed-command", diags)
	}
}

func TestLoadHooksRejectsDuplicateNestedKeys(t *testing.T) {
	fs := fstest.MapFS{"hooks.json": {Data: []byte(`{"command":"echo","event":"pre_read_code","metadata":{"name":"first","name":"second"}}`)}}
	if _, _, err := LoadHooks(fs, "hooks.json"); err == nil {
		t.Fatal("LoadHooks accepted duplicate nested JSON key")
	}
}

func TestLoadHooks_PreservesNativeEvents(t *testing.T) {
	hooks, diags, err := LoadHooks(testWorkspaceFS, ".windsurf/hooks")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	// Both events are declared in the Windsurf catalog, so ingestion keeps the
	// authored event names verbatim and emits no diagnostics.
	if len(hooks) != 2 {
		t.Fatalf("len(hooks) = %d, want 2 native hooks", len(hooks))
	}
	var read, native = -1, -1
	for i, hook := range hooks {
		if hook.HookSource != "windsurf" {
			t.Fatalf("hook %q HookSource = %q, want windsurf", hook.Name(), hook.HookSource)
		}
		switch hook.Event {
		case "pre_read_code":
			read = i
		case "post_setup_worktree":
			native = i
		}
	}
	if read < 0 || native < 0 {
		t.Fatalf("hooks = %#v, want pre_read_code and post_setup_worktree preserved", hooks)
	}
	if hooks[read].Matcher != "" {
		t.Fatalf("matcher = %q, want none (Windsurf events carry tool selection)", hooks[read].Matcher)
	}
	if hooks[native].Ext.Windsurf == nil || hooks[native].Ext.Windsurf.HookPowerShell != "Write-Host setup" || hooks[native].Ext.Windsurf.HookShowOutput == nil || *hooks[native].Ext.Windsurf.HookShowOutput || hooks[native].Ext.Windsurf.HookWorkingDir != "/tmp/project" {
		t.Fatalf("native hook fields = %#v", hooks[native].Ext.Windsurf)
	}
	if len(diags) != 0 {
		t.Fatalf("LoadHooks diags = %#v, want none for declared native events", diags)
	}
}

func TestLoadMcpServersRejectsDuplicateNestedKeys(t *testing.T) {
	fs := fstest.MapFS{"mcp.json": {Data: []byte(`{"mcpServers":{"server":{"env":{"TOKEN":"first","TOKEN":"second"}}}}`)}}
	if _, _, err := LoadMcpServers(fs, "mcp.json"); err == nil {
		t.Fatal("LoadMcpServers accepted duplicate nested JSON key")
	}
}

func TestLoadMcpServers_PreservesLiteralPaths(t *testing.T) {
	servers, diags, err := LoadMcpServers(testWorkspaceFS, ".windsurf/mcp_config.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	// ${env:GITHUB_TOKEN} should be normalized to ${GITHUB_TOKEN}
	if got := servers[0].Env["GITHUB_TOKEN"]; got != "${GITHUB_TOKEN}" {
		t.Fatalf("GITHUB_TOKEN env = %q, want %q", got, "${GITHUB_TOKEN}")
	}
	// Literal path preserved as-is
	if got := servers[0].Env["SETTINGS"]; got != "./settings.json" {
		t.Fatalf("SETTINGS env = %q, want %q", got, "./settings.json")
	}
	// Expect both the normalization diagnostic and the literal-path diagnostic
	var normCount, litCount int
	for _, d := range diags {
		if d.Code == diag.CodeIngestWindsurfMcpEnvNormalized {
			normCount++
		}
		if d.Code == diag.CodeIngestWindsurfLiteralPathPreserved {
			litCount++
		}
	}
	if normCount != 1 {
		t.Fatalf("normalization diagnostic count = %d, want 1", normCount)
	}
	if litCount != 1 {
		t.Fatalf("literal-path diagnostic count = %d, want 1", litCount)
	}
}

func TestLoadWorkspace_SynthesizesManifestAndLoadsResources(t *testing.T) {
	bundle, err := LoadWorkspace(testWorkspaceFS, LoadOptions{})
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	if len(bundle.Resources) != 5 {
		t.Fatalf("len(bundle.Resources) = %d, want 5 including preserved native hook", len(bundle.Resources))
	}

	var codes []string
	for _, d := range bundle.Diagnostics {
		codes = append(codes, d.Code)
	}
	sort.Strings(codes)
	want := []string{
		diag.CodeIngestWindsurfManifestSynthesized,
		diag.CodeIngestWindsurfLiteralPathPreserved,
		diag.CodeIngestWindsurfMcpEnvNormalized,
		diag.CodeIngestWindsurfSubagentViaAgentsMDSkipped,
		diag.CodeIngestWindsurfWorkflowAssumedCommand,
	}
	if len(codes) != len(want) {
		t.Fatalf("diagnostic count = %d, want %d (%v)", len(codes), len(want), codes)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("diag[%d] = %q, want %q (all=%v)", i, codes[i], want[i], codes)
		}
	}
}

func TestLoadWorkspaceUnconsumedAuditExcludesUserRoot(t *testing.T) {
	project := fstest.MapFS{
		".windsurf/mcp_config.json": {Data: []byte(`{"mcpServers":{"project":{"command":"project-command"}}}`)},
		".factory/mcp.json":         {Data: []byte(`{"mcpServers":{"foreign":{"command":"ignored-command"}}}`)},
	}
	user := fstest.MapFS{
		"mcp_config.json": {Data: []byte(`{"mcpServers":{"user":{"command":"user-command"}}}`)},
		".mcp.json":       {Data: []byte(`{"mcpServers":{"unrelated":{"command":"unrelated-command"}}}`)},
	}

	tests := []struct {
		name          string
		user          fs.FS
		src           fstest.MapFS
		wantWarnings  []string
		wantResources map[string]string
	}{
		{
			name:         "without user",
			wantWarnings: []string{".factory/mcp.json"},
			wantResources: map[string]string{
				"project": "project-command",
			},
		},
		{
			name:         "separate user",
			user:         user,
			wantWarnings: []string{".factory/mcp.json"},
			wantResources: map[string]string{
				"project": "project-command",
				"user":    "user-command",
			},
		},
		{
			name: "same filesystem",
			src: func() fstest.MapFS {
				combined := fstest.MapFS{}
				for name, file := range project {
					combined[name] = file
				}
				for name, file := range user {
					if name == ".mcp.json" {
						continue
					}
					combined[name] = file
				}
				return combined
			}(),
			wantWarnings: []string{".factory/mcp.json", "mcp_config.json"},
			wantResources: map[string]string{
				"project": "project-command",
				"user":    "user-command",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			src := test.src
			if src == nil {
				src = project
			}
			user := test.user
			if test.name == "same filesystem" {
				user = src
			}
			bundle, err := LoadWorkspace(src, LoadOptions{User: user})
			if err != nil {
				t.Fatalf("LoadWorkspace: %v", err)
			}
			resources := make(map[string]string)
			for _, resource := range bundle.Resources {
				if resource.Kind() != ir.KindMcpServer {
					continue
				}
				server := resource.(*ir.McpServer)
				resources[server.Name()] = server.Command
			}
			if !maps.Equal(resources, test.wantResources) {
				t.Fatalf("MCP resources = %v, want %v", resources, test.wantResources)
			}
			var warnings []string
			for _, diagnostic := range bundle.Diagnostics {
				if diagnostic.Code != diag.CodeIngestWindsurfUnconsumedConfig {
					continue
				}
				if diagnostic.Severity != diag.SeverityWarning || !strings.Contains(diagnostic.Message, "windsurf workspace") {
					t.Fatalf("unexpected audit diagnostic: %#v", diagnostic)
				}
				warnings = append(warnings, diagnostic.Source.File)
			}
			if !slices.Equal(warnings, test.wantWarnings) {
				t.Fatalf("audit warnings = %v, want %v", warnings, test.wantWarnings)
			}
		})
	}
}

func TestIngesterLoadBundleFromWorkspaceRoot(t *testing.T) {
	ing := Ingester{}
	bundle, err := ing.LoadBundle(testWorkspaceRootFS)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.Resources) == 0 {
		t.Fatal("len(bundle.Resources) = 0, want > 0")
	}
}

func TestNormalizeWindsurfEnvValue(t *testing.T) {
	tests := []struct {
		input   string
		output  string
		changed bool
	}{
		// ${env:VAR} → ${VAR}
		{"${env:GITHUB_TOKEN}", "${GITHUB_TOKEN}", true},
		{"${env:API_KEY}", "${API_KEY}", true},
		{"${env:MY_VAR_123}", "${MY_VAR_123}", true},
		{"${env:lowercase}", "${lowercase}", true},
		// Already canonical forms → passthrough
		{"${GITHUB_TOKEN}", "${GITHUB_TOKEN}", false},
		{"$GITHUB_TOKEN", "$GITHUB_TOKEN", false},
		// ${file:PATH} → passthrough
		{"${file:./settings.json}", "${file:./settings.json}", false},
		// Literal values → passthrough
		{"keep-me", "keep-me", false},
		{"./settings.json", "./settings.json", false},
		{"", "", false},
		// Non-matching ${env:...} patterns → passthrough
		{"${env:}", "${env:}", false},
		{"${env:123}", "${env:123}", false},
		// Embedded ${env:VAR} within larger string → normalize to ${VAR}
		{"prefix-${env:VAR}-suffix", "prefix-${VAR}-suffix", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, changed := normalizeWindsurfEnvValue(tt.input)
			if got != tt.output {
				t.Errorf("normalizeWindsurfEnvValue(%q) = %q, want %q", tt.input, got, tt.output)
			}
			if changed != tt.changed {
				t.Errorf("normalizeWindsurfEnvValue(%q) changed = %v, want %v", tt.input, changed, tt.changed)
			}
		})
	}
}

func TestLoadMcpServers_NormalizesWindsurfEnvInterpolation(t *testing.T) {
	fs := fstest.MapFS{
		"mcp_config.json": &fstest.MapFile{
			Data: []byte(`{
  "mcpServers": {
    "multi-env": {
      "command": "npx ${env:RUNNER}",
      "url": "https://${env:HOST}/mcp",
      "headers": {"Authorization": "Bearer ${env:TOKEN}"},
      "extra": {"nested": ["${env:NESTED}"]},
      "args": ["-y", "multi-server"],
      "env": {
        "TOKEN": "${env:MY_TOKEN}",
        "PATH_VAL": "${file:./data}",
        "LITERAL": "keep-me"
      }
    }
  }
}`),
		},
	}

	servers, diags, err := LoadMcpServers(fs, "mcp_config.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}

	if servers[0].Command != "npx ${RUNNER}" || servers[0].URL != "https://${HOST}/mcp" || servers[0].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("MCP canonical fields = command %q, url %q, headers %#v", servers[0].Command, servers[0].URL, servers[0].Headers)
	}
	if servers[0].Extra["extra"].(map[string]any)["nested"].([]any)[0] != "${NESTED}" {
		t.Fatalf("recursive extra was not normalized: %#v", servers[0].Extra)
	}
	// ${env:MY_TOKEN} normalized to ${MY_TOKEN}
	if got := servers[0].Env["TOKEN"]; got != "${MY_TOKEN}" {
		t.Errorf("TOKEN = %q, want %q", got, "${MY_TOKEN}")
	}
	// ${file:...} passthrough
	if got := servers[0].Env["PATH_VAL"]; got != "${file:./data}" {
		t.Errorf("PATH_VAL = %q, want %q", got, "${file:./data}")
	}
	// Literal value passthrough
	if got := servers[0].Env["LITERAL"]; got != "keep-me" {
		t.Errorf("LITERAL = %q, want %q", got, "keep-me")
	}

	// Check for normalization diagnostic
	var normDiags int
	for _, d := range diags {
		if d.Code == diag.CodeIngestWindsurfMcpEnvNormalized {
			normDiags++
		}
	}
	if normDiags != 1 {
		t.Errorf("normalization diagnostics = %d, want 1", normDiags)
	}
}

func TestLoadMcpServers_CanonicalEnvNotReNormalized(t *testing.T) {
	// If source already uses canonical ${VAR} form, no normalization occurs.
	fs := fstest.MapFS{
		"mcp_config.json": &fstest.MapFile{
			Data: []byte(`{
  "mcpServers": {
    "canonical": {
      "command": "npx",
      "env": {
        "TOKEN": "${MY_TOKEN}"
      }
    }
  }
}`),
		},
	}

	servers, diags, err := LoadMcpServers(fs, "mcp_config.json")
	if err != nil {
		t.Fatalf("LoadMcpServers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	if got := servers[0].Env["TOKEN"]; got != "${MY_TOKEN}" {
		t.Errorf("TOKEN = %q, want %q (unchanged)", got, "${MY_TOKEN}")
	}
	for _, d := range diags {
		if d.Code == diag.CodeIngestWindsurfMcpEnvNormalized {
			t.Errorf("unexpected normalization diagnostic for already-canonical value")
		}
	}
}

func TestDuplicateWorkspaceIdentityDoesNotFallBackToUserRoot(t *testing.T) {
	user := fstest.MapFS{
		"workflows/deploy.md": {Data: []byte("---\nname: deploy\ndescription: User deployment\n---\nINERT USER BODY\n")},
		"rules/style.md":      {Data: []byte("INERT USER RULE\n")},
	}
	project := fstest.MapFS{
		".windsurf/workflows/deploy.md": {Data: []byte("---\nname: deploy\ndescription: Approved deployment\n---\nINERT APPROVED BODY\n")},
		".windsurf/workflows/zzz.md":    {Data: []byte("---\nname: deploy\ndescription: Replacement deployment\n---\nINERT REPLACEMENT BODY\n")},
		".windsurf/rules/style.md":      {Data: []byte("INERT PROJECT RULE\n")},
		".windsurf/rules/style.mdc":     {Data: []byte("---\ndescription: Style\nalwaysApply: true\n---\nINERT PROJECT MDC RULE\n")},
	}
	bundle, err := LoadWorkspace(project, LoadOptions{User: user})
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	for _, resource := range bundle.Resources {
		if name := resource.Name(); name == "deploy" || name == "style" {
			t.Fatalf("ambiguous %s %q resolved to a resource: %+v", resource.Kind(), name, resource)
		}
	}
	refused := map[string]bool{}
	for _, d := range bundle.Diagnostics {
		if d.Code == diag.CodeIngestWindsurfDuplicateIdentity && d.Severity == diag.SeverityError {
			refused[d.Resource] = true
		}
	}
	if !refused["deploy"] || !refused["style"] {
		t.Fatalf("duplicate identities not reported: %+v", bundle.Diagnostics)
	}
}
