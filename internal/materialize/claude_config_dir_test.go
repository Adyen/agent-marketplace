package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func claudeDirectoryFixture(t *testing.T) (home, repo, custom string) {
	t.Helper()
	home, repo = t.TempDir(), t.TempDir()
	custom = filepath.Join(t.TempDir(), "account")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return
}

func claudeDirectoryWrite(t *testing.T, file, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeDirectoryRead(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func claudeDirectorySkill(name string, source config.Source, target hcc.Target) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Name: name, Kind: ir.KindSkill, Resource: ir.NewSkill(name, "fixture", "fixture body", nil, nil, ir.SourceRef{})},
		Source: source, Harnesses: []hcc.Target{target},
	}
}

// spec: claude-native-configuration/machine-selections-install-under-the-configuration-directory
// spec: materialization/relocated-claude-code-machine-root-is-admitted
// spec: claude-native-configuration/removing-the-last-mcp-server-keeps-the-configuration-file
func TestClaudeConfigurationDirectoryLifecycle(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, seeded := range []bool{false, true} {
			t.Run(string(mode)+"/"+map[bool]string{false: "missing", true: "existing"}[seeded], func(t *testing.T) {
				home, repo, custom := claudeDirectoryFixture(t)
				defaultFile := filepath.Join(home, ".claude.json")
				defaultRaw := `{"numStartups":42,"mcpServers":{"mine-home":{"command":"true"}}}`
				claudeDirectoryWrite(t, defaultFile, defaultRaw)
				file := filepath.Join(custom, ".claude.json")
				original := "{\n // keep account settings\n \"numStartups\":7,\n \"projects\":{\"mine\":{\"_managed_by\":\"agent-marketplace\",\"trust\":true}},\n \"mcpServers\":{\"mine-alt\":{\"command\":\"true\"}}\n}\n"
				wantMode := os.FileMode(0o600)
				if seeded {
					claudeDirectoryWrite(t, file, original)
					wantMode = 0o640
					if err := os.Chmod(file, wantMode); err != nil {
						t.Fatal(err)
					}
				}
				hook := resolverHook(t, makeHookSupportSource(t, "echo guard", 0o600), "guard")
				hook.Source, hook.Harnesses = config.UserPersonal, []hcc.Target{hcc.TargetClaude}
				resources := []resolver.ResolvedArtifact{
					claudeDirectorySkill("hello", config.UserPersonal, hcc.TargetClaude),
					{Entry: catalog.Entry{Name: "greet", Kind: ir.KindCommand, Resource: ir.NewCommand("greet", "greet", "hello", nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetClaude}},
					{Entry: catalog.Entry{Name: "reviewer", Kind: ir.KindSubagent, Resource: ir.NewSubagent("reviewer", "review", "review", "", nil, "", nil, "", nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetClaude}},
					hook,
				}
				mcp := nativeMCPServerResources(t, hcc.TargetClaude, config.UserPersonal)[:1]
				resources = append(resources, mcp...)
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Machine: mode, Project: mode})}
				report, err := Run(context.Background(), repo, resources, options...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("sync: %v, %+v", err, report)
				}
				for _, rel := range []string{"skills/hello/SKILL.md", "commands/greet.md", "agents/reviewer.md", ".claude-plugin/plugin.json", "settings.json", ".claude.json"} {
					if len(claudeDirectoryRead(t, filepath.Join(custom, rel))) == 0 {
						t.Fatalf("empty output %s", rel)
					}
				}
				if matches, err := filepath.Glob(filepath.Join(custom, "hook-support", "*", "guard", "hooks", "shared.sh")); err != nil || len(matches) != 1 {
					t.Fatalf("hook support = %v, %v", matches, err)
				}
				after := claudeDirectoryRead(t, file)
				if !bytes.Contains(after, []byte(`"first"`)) || (seeded && (!bytes.Contains(after, []byte("// keep account settings")) || !bytes.Contains(after, []byte(`"mine-alt"`)))) {
					t.Fatalf("MCP or handwritten state lost: %s", after)
				}
				beforeInfo, _ := os.Stat(file)
				report, err = Run(context.Background(), repo, resources, append(options, WithDryRun())...)
				if err != nil || !report.InSync() {
					t.Fatalf("check: %v, %+v", err, report)
				}
				// Removing all selections also prunes real installed resource output.
				report, err = Run(context.Background(), repo, nil, options...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("remove: %v, %+v", err, report)
				}
				after = claudeDirectoryRead(t, file)
				if bytes.Contains(after, []byte(`"first"`)) || (seeded && !bytes.Contains(after, []byte(`"mine-alt"`))) {
					t.Fatalf("last MCP removal: %s", after)
				}
				if _, err := os.Lstat(filepath.Join(custom, "skills", "hello")); !os.IsNotExist(err) {
					t.Fatalf("stale resource not pruned: %v", err)
				}
				// The root walk must not strip markers in unrelated account state.
				report, err = Run(context.Background(), repo, nil, options...)
				if err != nil || !report.InSync() || !bytes.Equal(claudeDirectoryRead(t, file), after) {
					t.Fatalf("prune changed account state: %v, %+v", err, report)
				}
				if string(claudeDirectoryRead(t, defaultFile)) != defaultRaw {
					t.Fatal("default account changed")
				}
				if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
					t.Fatalf("default root created: %v", err)
				}
				if beforeInfo.Mode().Perm() != wantMode {
					t.Fatalf("MCP mode = %o", beforeInfo.Mode().Perm())
				}
			})
		}
	}
}

// spec: claude-native-configuration/unmanaged-collision-in-the-relocated-file-fails-closed
func TestClaudeConfigurationDirectoryMCPRefusal(t *testing.T) {
	for _, raw := range []string{`{"mcpServers":{"first":{"command":"manual"}}}`, `{"malformed":`} {
		t.Run(raw, func(t *testing.T) {
			_, repo, custom := claudeDirectoryFixture(t)
			file := filepath.Join(custom, ".claude.json")
			claudeDirectoryWrite(t, file, raw)
			resources := append(nativeMCPServerResources(t, hcc.TargetClaude, config.UserPersonal),
				claudeDirectorySkill("sibling", config.UserPersonal, hcc.TargetClaude))
			report, err := Run(context.Background(), repo, resources)
			if err != nil || !diag.HasErrors(report.Diags) || string(claudeDirectoryRead(t, file)) != raw {
				t.Fatalf("unsafe MCP update: %v, %+v", err, report)
			}
			if !strings.Contains(string(claudeDirectoryRead(t, filepath.Join(custom, "skills/sibling/SKILL.md"))), "fixture body") {
				t.Fatal("safe sibling did not install")
			}
		})
	}
}

// spec: claude-native-configuration/pruning-the-relocated-root-preserves-the-configuration-file
func TestClaudeConfigurationDirectoryPruneFence(t *testing.T) {
	_, repo, custom := claudeDirectoryFixture(t)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		claudeDirectorySkill("stale", config.UserPersonal, hcc.TargetClaude),
	})
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("seed stale output: %v, %+v", err, report)
	}
	file := filepath.Join(custom, ".claude.json")
	original := "{\n // handwritten account\n \"projects\":{\"mine\":{\"_managed_by\":\"agent-marketplace\",\"trust\":true}},\n \"mcpServers\":{\"manual\":{\"command\":\"true\"}}\n}\n"
	claudeDirectoryWrite(t, file, original)
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	report, err = Run(context.Background(), repo, nil)
	if err != nil || diag.HasErrors(report.Diags) || len(report.PerTarget[hcc.TargetClaude].Removed) == 0 {
		t.Fatalf("prune did not remove stale output: %v, %+v", err, report)
	}
	if string(claudeDirectoryRead(t, file)) != original {
		t.Fatal("pruning changed handwritten account state")
	}
	after, err := os.Stat(file)
	if err != nil || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatalf("pruning changed configuration metadata: %v", err)
	}
}
