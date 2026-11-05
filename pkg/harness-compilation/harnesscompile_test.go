package harnesscompile

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestCompile_WithPluginRoot(t *testing.T) {
	hook := ir.NewHookWithMatcher(
		"PreToolUse-Read-command",
		"PreToolUse",
		"Read",
		"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh",
		"command",
		nil,
		ir.SourceRef{File: "hooks/hooks.json", Line: 0},
	)

	result, err := Compile(hook, TargetWindsurf, WithPluginRoot("/plugins/acme"))
	if err != nil {
		t.Fatalf("Compile(hook): %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("Compile(hook) artifacts = %d, want 1", len(result.Artifacts))
	}
	got := string(result.Artifacts[0].Bytes)
	// Windsurf repository commands run from the workspace root, so the
	// catalog-driven resolver emits the repository-relative support path.
	if want := "plugins/acme/scripts/check.sh"; !strings.Contains(got, want) {
		t.Fatalf("artifact does not contain expanded plugin root %q:\n%s", want, got)
	}
}

func TestCompileMcpCredentialDiagnosticsForCanonicalExtras(t *testing.T) {
	const literalSecret = "literal-extension-secret"
	tests := []struct {
		name        string
		extra       map[string]any
		wantLiteral bool
	}{
		{
			name:        "environment literal",
			extra:       map[string]any{"env": map[string]any{"API_TOKEN": literalSecret}},
			wantLiteral: true,
		},
		{
			name:        "header literal",
			extra:       map[string]any{"headers": map[string]any{"Authorization": "Bearer " + literalSecret}},
			wantLiteral: true,
		},
		{
			name:        "OAuth literal",
			extra:       map[string]any{"oauthClientSecret": literalSecret},
			wantLiteral: true,
		},
		{
			name: "interpolations",
			extra: map[string]any{
				"env":               map[string]any{"API_TOKEN": "${API_TOKEN}"},
				"headers":           map[string]any{"Authorization": "Bearer ${API_TOKEN}"},
				"oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}",
			},
		},
	}
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, tt := range tests {
		for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetWindsurf, TargetGemini, TargetDevin, TargetCodex} {
			t.Run(tt.name+"/"+string(target), func(t *testing.T) {
				var logs bytes.Buffer
				slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
				server := ir.NewMcpServer("credentials", "server", nil, nil, tt.extra, ir.SourceRef{})
				result, err := Compile(server, target)
				if err != nil {
					t.Fatalf("Compile(mcp, %s): %v", target, err)
				}
				if got := diag.HasCode(result.Diagnostics, diag.CodeMcpLiteralCredential); got != tt.wantLiteral {
					t.Fatalf("literal credential diagnostic = %v, want %v: %#v", got, tt.wantLiteral, result.Diagnostics)
				}
				if !tt.wantLiteral {
					if strings.Contains(logs.String(), "literal MCP credential detected") {
						t.Fatalf("interpolation produced literal credential log: %s", logs.String())
					}
					return
				}
				if !strings.Contains(logs.String(), `"msg":"literal MCP credential detected"`) {
					t.Fatalf("literal credential log missing: %s", logs.String())
				}
				for _, diagnostic := range result.Diagnostics {
					if strings.Contains(diagnostic.Message, literalSecret) || strings.Contains(diagnostic.Hint, literalSecret) {
						t.Fatalf("credential value leaked into diagnostic: %#v", diagnostic)
					}
				}
				if strings.Contains(logs.String(), literalSecret) {
					t.Fatalf("credential value leaked into log: %s", logs.String())
				}
			})
		}
	}
}

// --- Phase 2: Harness alias, Convert, IngestBundle, Autodetect tests ---

func TestHarnessAliasAndConstants(t *testing.T) {
	// Harness is a type alias for Target
	h := Harness(TargetClaude)
	_ = h

	// Verify Harness constants match Target constants
	tests := []struct {
		harness Harness
		target  Target
	}{
		{HarnessClaude, TargetClaude},
		{HarnessFactory, TargetFactory},
		{HarnessCursor, TargetCursor},
		{HarnessWindsurf, TargetWindsurf},
		{HarnessGemini, TargetGemini},
		{HarnessDevin, TargetDevin},
	}
	for _, tt := range tests {
		if tt.harness != Harness(tt.target) {
			t.Errorf("Harness constant %q does not match Target constant %q", tt.harness, tt.target)
		}
	}

	// Verify string values
	wantValues := map[Harness]string{
		HarnessClaude:   "claude",
		HarnessFactory:  "factory",
		HarnessCursor:   "cursor",
		HarnessWindsurf: "windsurf",
		HarnessGemini:   "gemini",
		HarnessDevin:    "devin",
	}
	for h, want := range wantValues {
		if string(h) != want {
			t.Errorf("Harness %q = %q, want %q", h, string(h), want)
		}
	}
}

func TestConvert_UnregisteredSource_ReturnsNotImplementedError(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}

	// A harness with no registered ingester should still return errSourceNotImplemented.
	_, err := Convert(src, Harness("unknown"), HarnessClaude)
	if err == nil {
		t.Fatalf("Convert from unregistered harness should return error")
	}
	if !errors.Is(err, errSourceNotImplemented) {
		t.Fatalf("Convert from unregistered harness error = %v, want errSourceNotImplemented", err)
	}
}

func TestConvert_AllRegisteredSources(t *testing.T) {
	// Verify that Convert works for all 6 registered source harnesses
	// when given appropriate input fixtures.
	testCases := []struct {
		from Harness
		fs   fstest.MapFS
	}{
		{
			from: HarnessClaude,
			fs: fstest.MapFS{
				"skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
				},
			},
		},
		{
			from: HarnessFactory,
			fs: fstest.MapFS{
				".factory-plugin/plugin.json": &fstest.MapFile{
					Data: []byte(`{"skills":[{"name":"my-skill","description":""}]}`),
				},
				"skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("Do the thing\n"),
				},
			},
		},
		{
			from: HarnessCursor,
			fs: fstest.MapFS{
				".cursor-plugin/plugin.json": &fstest.MapFile{
					Data: []byte(`{"name":"cursor-plugin"}`),
				},
				"skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
				},
			},
		},
		{
			from: HarnessWindsurf,
			fs: fstest.MapFS{
				".windsurf/skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
				},
			},
		},
		{
			from: HarnessGemini,
			fs: fstest.MapFS{
				"gemini-extension.json": &fstest.MapFile{
					Data: []byte(`{"name":"gemini-plugin"}`),
				},
				".gemini/skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
				},
			},
		},
		{
			from: HarnessDevin,
			fs: fstest.MapFS{
				".devin/skills/my-skill/SKILL.md": &fstest.MapFile{
					Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
				},
			},
		},
	}

	for _, tc := range testCases {
		for _, to := range Targets() {
			t.Run(string(tc.from)+"-to-"+string(to), func(t *testing.T) {
				result, err := Convert(tc.fs, tc.from, to)
				if err != nil {
					t.Fatalf("Convert(%s→%s): %v", tc.from, to, err)
				}
				if result == nil {
					t.Fatal("result must not be nil")
				}
			})
		}
	}
}

func TestConvertExplicitCodexReadsSharedSkillWithoutCodexMarker(t *testing.T) {
	src := fstest.MapFS{
		".agents/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")},
	}
	result, err := Convert(src, HarnessCodex, HarnessCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Path != ".agents/skills/demo/SKILL.md" {
		t.Fatalf("Convert(Codex→Codex) artifacts = %#v, want explicit shared skill", result.Artifacts)
	}
}

func TestConvert_WindsurfRepoRootToFactory_ProducesArtifacts(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
	}

	result, err := Convert(src, HarnessWindsurf, HarnessFactory)
	if err != nil {
		t.Fatalf("Convert(windsurf→factory): %v", err)
	}
	if len(result.Artifacts) == 0 {
		t.Fatal("Convert(windsurf→factory) produced 0 artifacts, want > 0")
	}

	found := false
	for _, artifact := range result.Artifacts {
		if artifact.Path == "skills/my-skill/SKILL.md" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Convert(windsurf→factory) missing expected artifact %q; got %v", "skills/my-skill/SKILL.md", result.Artifacts)
	}
}

func TestConvert_WindsurfWorkspaceRootToFactory_ProducesArtifacts(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
	}

	result, err := Convert(src, HarnessWindsurf, HarnessFactory)
	if err != nil {
		t.Fatalf("Convert(windsurf-root→factory): %v", err)
	}
	if len(result.Artifacts) == 0 {
		t.Fatal("Convert(windsurf-root→factory) produced 0 artifacts, want > 0")
	}
}

func TestConvert_ClaudeSource_MatchesCompileBundle(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		"commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
		},
	}

	targets := Targets()
	for _, to := range targets {
		t.Run(string(to), func(t *testing.T) {
			convertResult, err := Convert(src, HarnessClaude, to)
			if err != nil {
				t.Fatalf("Convert(Claude→%s): %v", to, err)
			}

			bundleResult, err := CompileBundle(src, Target(to))
			if err != nil {
				t.Fatalf("CompileBundle(%s): %v", to, err)
			}

			if len(convertResult.Artifacts) != len(bundleResult.Artifacts) {
				t.Fatalf("artifact count: Convert=%d, CompileBundle=%d", len(convertResult.Artifacts), len(bundleResult.Artifacts))
			}
		})
	}
}

func TestIngestBundle_UnregisteredSource_ReturnsNotImplementedError(t *testing.T) {
	src := fstest.MapFS{}

	_, err := IngestBundle(src, Harness("unknown"))
	if err == nil {
		t.Fatalf("IngestBundle from unregistered harness should return error")
	}
	if !errors.Is(err, errSourceNotImplemented) {
		t.Fatalf("IngestBundle from unregistered harness error = %v, want errSourceNotImplemented", err)
	}
}

func TestIngestBundle_ClaudeSource_ReturnsBundle(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		"commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
		},
	}

	b, err := IngestBundle(src, HarnessClaude)
	if err != nil {
		t.Fatalf("IngestBundle(Claude): %v", err)
	}
	if b == nil {
		t.Fatal("IngestBundle(Claude) returned nil bundle")
	}
	if len(b.Resources) == 0 {
		t.Fatal("IngestBundle(Claude) returned no resources")
	}

	// Verify we got at least a skill and a command
	kinds := make(map[ir.ResourceKind]bool)
	for _, r := range b.Resources {
		kinds[r.Kind()] = true
	}
	if !kinds[ir.KindSkill] {
		t.Error("expected Skill resource in bundle")
	}
	if !kinds[ir.KindCommand] {
		t.Error("expected Command resource in bundle")
	}
}

func TestAutodetect_ClaudePluginForm(t *testing.T) {
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Claude plugin form")
	}
	if h != HarnessClaude {
		t.Fatalf("Autodetect = %q, want %q", h, HarnessClaude)
	}
}

func TestAutodetect_ClaudeWorkspaceForm(t *testing.T) {
	src := fstest.MapFS{
		".claude/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\n---\nBody"),
		},
	}
	h, ok := Autodetect(src)
	if !ok {
		t.Fatal("Autodetect should detect Claude workspace form")
	}
	if h != HarnessClaude {
		t.Fatalf("Autodetect = %q, want %q", h, HarnessClaude)
	}
}

func TestAutodetect_NoMatch(t *testing.T) {
	src := fstest.MapFS{}
	h, ok := Autodetect(src)
	if ok {
		t.Fatalf("Autodetect should not detect anything in empty FS, got %q", h)
	}
	if h != "" {
		t.Fatalf("Autodetect should return empty harness on no match, got %q", h)
	}
}

func TestCompileBundle_Regression_AfterConvertRefactor(t *testing.T) {
	// Verify CompileBundle still works after the Convert refactor.
	src := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
		},
		"commands/my-cmd.md": &fstest.MapFile{
			Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
		},
		"agents/my-agent.md": &fstest.MapFile{
			Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\ntools:\n  - Read\n---\nYou are an agent"),
		},
		"hooks/hooks.json": &fstest.MapFile{
			Data: []byte(`{"hooks":{"PreToolUse":[{"type":"command","command":"echo check","matcher":"Read"}]}}`),
		},
		".mcp.json": &fstest.MapFile{
			Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
		},
	}

	for _, target := range Targets() {
		t.Run(string(target), func(t *testing.T) {
			result, err := CompileBundle(src, target)
			if err != nil {
				t.Fatalf("CompileBundle(%s): %v", target, err)
			}
			if result == nil {
				t.Fatal("result must not be nil")
			}
		})
	}
}
