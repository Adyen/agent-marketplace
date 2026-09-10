package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func nativeMCPResources(target hcc.Target, source config.Source) []resolver.ResolvedArtifact {
	var result []resolver.ResolvedArtifact
	for _, name := range []string{"first", "second"} {
		resource := ir.NewMcpServer(name, "echo", []string{name}, nil, nil, ir.SourceRef{})
		result = append(result, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Kind: ir.KindMcpServer, Resource: resource}, Source: source, Harnesses: []hcc.Target{target}})
	}
	if target == hcc.TargetGemini {
		resource := ir.NewHook("audit", "PostToolUse", "echo audit", "command", nil, ir.SourceRef{})
		result = append(result, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "audit", Kind: ir.KindHook, Resource: resource}, Source: source, Harnesses: []hcc.Target{target}})
	}
	return result
}

func TestNativeMCPLifecycle(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, machine := range []bool{false, true} {
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				source := config.RepoCommitted
				scope := "repo"
				if machine {
					source = config.UserPersonal
					scope = "user"
				}
				t.Run(string(target)+"/"+scope+"/"+string(mode), func(t *testing.T) {
					home, repo := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("XDG_STATE_HOME", t.TempDir())
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
					base := repo
					if machine {
						base = home
					}
					rel, legacy := ".mcp.json", ".claude/.mcp.json"
					if machine {
						rel = ".claude.json"
					}
					if target == hcc.TargetGemini {
						rel, legacy = ".gemini/settings.json", ".gemini/gemini-extension.json"
					}
					destination := filepath.Join(base, rel)
					if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
						t.Fatal(err)
					}
					original := []byte("{\n  // handwritten settings\n  \"theme\": \"dark\",\n  \"projects\": {\"manual\": {\"_managed_by\": \"agent-marketplace\", \"trust\": false}},\n  \"mcpServers\": {\"manual\": {\"command\": \"manual-tool\"}},\n  \"hooks\": {\"AfterTool\": [{\"matcher\": \"manual\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo manual\"}]}]}\n}\n")
					if err := os.WriteFile(destination, original, 0o600); err != nil {
						t.Fatal(err)
					}
					sentinel := filepath.Join(base, "unrelated.json")
					sentinelBytes := []byte(`{"mcpServers":{"unrelated":{"_managed_by":"agent-marketplace","command":"keep"}}}`)
					if err := os.WriteFile(sentinel, sentinelBytes, 0o600); err != nil {
						t.Fatal(err)
					}
					modes := config.MaterializationModes{Machine: mode, Project: mode}
					opts := []RunOption{WithMaterializationModes(modes)}
					desired := nativeMCPResources(target, source)
					report, err := Run(context.Background(), repo, desired, opts...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("sync: %v, %+v", err, report)
					}
					after, err := os.ReadFile(destination)
					if err != nil {
						t.Fatal(err)
					}
					values := nativeMCPDocument(t, after)
					servers := values["mcpServers"].(map[string]any)
					if len(servers) != 3 || servers["first"] == nil || servers["second"] == nil || servers["manual"] == nil {
						t.Fatalf("native servers = %v", servers)
					}
					for _, fragment := range [][]byte{[]byte("// handwritten settings"), []byte(`"theme": "dark"`), []byte(`"projects": {"manual": {"_managed_by": "agent-marketplace", "trust": false}}`), []byte(`"command": "echo manual"`)} {
						if !bytes.Contains(after, fragment) {
							t.Fatalf("lost handwritten content %s: %s", fragment, after)
						}
					}
					if target == hcc.TargetGemini && !bytes.Contains(after, []byte("echo audit")) {
						t.Fatalf("missing generated hook: %s", after)
					}
					if _, err := os.Lstat(filepath.Join(base, legacy)); !os.IsNotExist(err) {
						t.Fatalf("legacy destination written: %s (%v)", legacy, err)
					}
					for _, dry := range []bool{false, true} {
						runOpts := append([]RunOption{}, opts...)
						if dry {
							runOpts = append(runOpts, WithDryRun())
						}
						report, err = Run(context.Background(), repo, desired, runOpts...)
						if err != nil || !report.InSync() {
							t.Fatalf("second sync/check: %v %+v", err, report)
						}
						got, _ := os.ReadFile(destination)
						if !bytes.Equal(got, after) {
							t.Fatalf("second sync/check rewrote native config")
						}
					}
					report, err = Run(context.Background(), repo, nil, append(opts, WithDryRun())...)
					if err != nil || report.InSync() {
						t.Fatalf("removal check missed drift: %v %+v", err, report)
					}
					got, _ := os.ReadFile(destination)
					if !bytes.Equal(got, after) {
						t.Fatal("check modified config")
					}
					report, err = Run(context.Background(), repo, nil, opts...)
					if err != nil || hasErrorDiagnostics(report.Diags) {
						t.Fatalf("prune: %v %+v", err, report)
					}
					got, _ = os.ReadFile(destination)
					servers = nativeMCPDocument(t, got)["mcpServers"].(map[string]any)
					if len(servers) != 1 || servers["manual"] == nil || bytes.Contains(got, []byte("echo audit")) {
						t.Fatalf("prune = %s", got)
					}
					if !bytes.Contains(got, []byte(`"trust": false`)) || !bytes.Contains(got, []byte("echo manual")) {
						t.Fatalf("prune damaged settings: %s", got)
					}
					got, _ = os.ReadFile(sentinel)
					if !bytes.Equal(got, sentinelBytes) {
						t.Fatalf("scanned outside allowlisted files: %s", got)
					}
				})
			}
		}
	}
}

func TestNativeMCPSymlinksAndLegacyFiles(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			native, legacy := filepath.Join(repo, ".mcp.json"), filepath.Join(repo, ".claude/.mcp.json")
			if target == hcc.TargetGemini {
				native, legacy = filepath.Join(repo, ".gemini/settings.json"), filepath.Join(repo, ".gemini/gemini-extension.json")
			}
			if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
				t.Fatal(err)
			}
			legacyBytes := []byte(`{"mcpServers":{"old":{"command":"legacy","_managed_by":"agent-marketplace"},"manual":{"command":"manual"}}}`)
			if err := os.WriteFile(legacy, legacyBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "external.json")
			original := []byte(`{"mcpServers":{"outside":{"command":"untouched"}}}`)
			if err := os.WriteFile(external, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, native); err != nil {
				t.Fatal(err)
			}
			for _, dry := range []bool{false, true} {
				var opts []RunOption
				if dry {
					opts = append(opts, WithDryRun())
				}
				report, err := Run(context.Background(), repo, nativeMCPResources(target, config.RepoCommitted), opts...)
				if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
					t.Fatalf("symlink not refused: %v %+v", err, report)
				}
				if link, err := os.Readlink(native); err != nil || link != external {
					t.Fatalf("symlink changed: %s %v", link, err)
				}
				got, _ := os.ReadFile(external)
				if !bytes.Equal(got, original) {
					t.Fatal("external bytes changed")
				}
				got, _ = os.ReadFile(legacy)
				if !bytes.Equal(got, legacyBytes) {
					t.Fatal("legacy file changed")
				}
			}
			if err := os.Remove(native); err != nil {
				t.Fatal(err)
			}
			report, err := Run(context.Background(), repo, nativeMCPResources(target, config.RepoCommitted))
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("safe native install failed: %v %+v", err, report)
			}
			if _, err := os.Stat(native); err != nil {
				t.Fatal(err)
			}
			report, err = Run(context.Background(), repo, nil)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("prune failed: %v %+v", err, report)
			}
			got, _ := os.ReadFile(legacy)
			if !bytes.Equal(got, legacyBytes) {
				t.Fatal("prune modified legacy file")
			}
		})
	}
}

func TestNativeMCPCompileFailureWritesValidSiblingsWithoutPruning(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
			t.Run(string(target)+"/"+source.String(), func(t *testing.T) {
				home, repo := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				base, rel := repo, ".mcp.json"
				if source == config.UserPersonal {
					base, rel = home, ".claude.json"
				}
				if target == hcc.TargetGemini {
					rel = ".gemini/settings.json"
				}
				destination := filepath.Join(base, rel)
				prior := nativeMCPResources(target, source)
				report, err := Run(context.Background(), repo, prior)
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("initial sync: %v %+v", err, report)
				}
				before, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				valid := ir.NewMcpServer("third", "echo", []string{"third"}, nil, nil, ir.SourceRef{})
				updated := ir.NewMcpServer("first", "echo", []string{"updated"}, nil, nil, ir.SourceRef{})
				restricted := ir.NewMcpServer("second", "echo", nil, nil, nil, ir.SourceRef{})
				restricted.Ext.Codex = &ext.Codex{MCP: &ext.CodexMCP{Command: "echo", RemoteEnvVars: []string{"REMOTE_SETTING"}}}
				wanted := []resolver.ResolvedArtifact{
					{Entry: catalog.Entry{Name: "third", Kind: ir.KindMcpServer, Resource: valid}, Source: source, Harnesses: []hcc.Target{target}},
					{Entry: catalog.Entry{Name: "first", Kind: ir.KindMcpServer, Resource: updated}, Source: source, Harnesses: []hcc.Target{target}},
					{Entry: catalog.Entry{Name: "second", Kind: ir.KindMcpServer, Resource: restricted}, Source: source, Harnesses: []hcc.Target{target}},
				}
				if target == hcc.TargetGemini {
					hook := ir.NewHook("new-audit", "PostToolUse", "echo new-audit", "command", nil, ir.SourceRef{})
					wanted = append(wanted, resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "new-audit", Kind: ir.KindHook, Resource: hook}, Source: source, Harnesses: []hcc.Target{target}})
				}
				report, err = Run(context.Background(), repo, wanted, WithDryRun())
				if err != nil || !hasDiagCode(report.Diags, "mcp.security-restriction-unsupported") {
					t.Fatalf("missing compile refusal: %v %+v", err, report)
				}
				if len(report.PerTarget[target].Written) == 0 {
					t.Fatal("check hid valid sibling drift")
				}
				got, err := os.ReadFile(destination)
				if err != nil || !bytes.Equal(got, before) {
					t.Fatalf("check wrote native file: %v", err)
				}
				report, err = Run(context.Background(), repo, wanted)
				if err != nil || !hasDiagCode(report.Diags, "mcp.security-restriction-unsupported") {
					t.Fatalf("missing compile refusal: %v %+v", err, report)
				}
				got, err = os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				servers := nativeMCPDocument(t, got)["mcpServers"].(map[string]any)
				if len(servers) != 3 || servers["third"] == nil || servers["second"] == nil || !bytes.Contains(got, []byte("updated")) {
					t.Fatalf("valid siblings were suppressed or stale entries pruned: %s", got)
				}
				if !reflect.DeepEqual(servers["second"], nativeMCPDocument(t, before)["mcpServers"].(map[string]any)["second"]) {
					t.Fatalf("failed sibling was changed: %s", got)
				}
				if target == hcc.TargetGemini && (!bytes.Contains(got, []byte("echo audit")) || !bytes.Contains(got, []byte("echo new-audit"))) {
					t.Fatalf("hook siblings were suppressed or pruned: %s", got)
				}
				report, err = Run(context.Background(), repo, wanted)
				after, readErr := os.ReadFile(destination)
				if err != nil || readErr != nil || !hasDiagCode(report.Diags, "mcp.security-restriction-unsupported") || !bytes.Equal(after, got) {
					t.Fatalf("blocked retry changed config: %v %v %+v", err, readErr, report)
				}
				clean := append([]resolver.ResolvedArtifact(nil), wanted[:2]...)
				if target == hcc.TargetGemini {
					clean = append(clean, wanted[3])
				}
				report, err = Run(context.Background(), repo, clean)
				if err != nil || hasErrorDiagnostics(report.Diags) {
					t.Fatalf("clean sync: %v %+v", err, report)
				}
				got, err = os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				servers = nativeMCPDocument(t, got)["mcpServers"].(map[string]any)
				if len(servers) != 2 || servers["second"] != nil || bytes.Contains(got, []byte("echo audit")) {
					t.Fatalf("clean sync did not resume prune: %s", got)
				}
			})
		}
	}
}

func TestNativeMCPUnmanagedFilesDoNotAffectOtherTargets(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
			for _, original := range []string{`{"mcpServers":`, `{"projects":{"manual":{"_managed_by":"agent-marketplace"}},"mcpServers":`, `{"projects":{"manual":{"_managed_by":"agent-marketplace"}},"mcpServers":[]}`} {
				t.Run(string(target)+"/"+source.String()+"/"+original, func(t *testing.T) {
					home, repo := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("XDG_STATE_HOME", t.TempDir())
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
					base, rel := repo, ".mcp.json"
					if source == config.UserPersonal {
						base, rel = home, ".claude.json"
					}
					if target == hcc.TargetGemini {
						rel = ".gemini/settings.json"
					}
					native := filepath.Join(base, rel)
					if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(native, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
					wanted := nativeMCPResources(hcc.TargetFactory, source)
					for _, dry := range []bool{true, false, true} {
						var opts []RunOption
						if dry {
							opts = append(opts, WithDryRun())
						}
						report, err := Run(context.Background(), repo, wanted, opts...)
						if err != nil || hasErrorDiagnostics(report.Diags) {
							t.Fatalf("unmanaged native file broke unrelated sync: %v %+v", err, report)
						}
						for _, d := range report.Diags {
							if d.Target == string(target) {
								t.Fatalf("unmanaged native file produced diagnostic: %+v", d)
							}
						}
						got, err := os.ReadFile(native)
						if err != nil || string(got) != original {
							t.Fatalf("unmanaged bytes changed: %s %v", got, err)
						}
					}
					got, err := os.ReadFile(filepath.Join(base, ".factory/mcp.json"))
					if err != nil || !bytes.Contains(got, []byte("first")) || !bytes.Contains(got, []byte("second")) {
						t.Fatalf("unrelated target did not materialize: %s %v", got, err)
					}
					if target == hcc.TargetClaude {
						if _, err := os.Stat(filepath.Join(base, ".claude")); !os.IsNotExist(err) {
							t.Fatalf("unmanaged file created target root: %v", err)
						}
						if err := os.MkdirAll(filepath.Join(base, ".claude"), 0o755); err != nil {
							t.Fatal(err)
						}
						for _, dry := range []bool{false, true} {
							var opts []RunOption
							if dry {
								opts = append(opts, WithDryRun())
							}
							report, err := Run(context.Background(), repo, wanted, opts...)
							if err != nil || !report.InSync() {
								t.Fatalf("existing Claude root caused unrelated drift/error: %v %+v", err, report)
							}
							got, err := os.ReadFile(native)
							if err != nil || string(got) != original {
								t.Fatalf("existing root changed unmanaged native bytes: %s %v", got, err)
							}
						}
					}
				})
			}
		}
	}
}

func TestNativeMCPManagedFilesPruneDuringOtherTargetSync(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			report, err := Run(context.Background(), repo, nativeMCPResources(target, config.UserPersonal))
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("initial sync: %v %+v", err, report)
			}
			native := filepath.Join(home, ".claude.json")
			if target == hcc.TargetGemini {
				native = filepath.Join(home, ".gemini/settings.json")
			} else if err := os.Remove(filepath.Join(home, ".claude")); err != nil {
				t.Fatal(err)
			}
			report, err = Run(context.Background(), repo, nativeMCPResources(hcc.TargetFactory, config.UserPersonal))
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("unrelated sync: %v %+v", err, report)
			}
			got, err := os.ReadFile(native)
			if err != nil {
				t.Fatal(err)
			}
			servers := nativeMCPDocument(t, got)["mcpServers"].(map[string]any)
			if len(servers) != 0 || bytes.Contains(got, []byte("echo audit")) {
				t.Fatalf("removed managed native entries survived: %s", got)
			}
		})
	}
}

func nativeMCPDocument(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	doc, err := jsonc.Parse("native.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := doc.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNativeMCPRefusalsPreserveBytes(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetGemini} {
		for _, test := range []struct{ raw, code string }{
			{`{"mcpServers":`, "json.malformed"},
			{`{"mcpServers":[]}`, "materialize.write.error"},
			{`null`, "materialize.write.error"},
			{`{"mcpServers":{"first":{"command":"manual"}}}`, "materialize.collision.user-owned"},
			{`{"mcpServers":{"first":{},"first":{}}}`, "json.duplicate-key"},
		} {
			original := test.raw
			t.Run(string(target)+"/"+original, func(t *testing.T) {
				home, repo := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				rel := ".claude.json"
				if target == hcc.TargetGemini {
					rel = ".gemini/settings.json"
				}
				destination := filepath.Join(home, rel)
				if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(destination, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				report, err := Run(context.Background(), repo, nativeMCPResources(target, config.UserPersonal))
				if err != nil {
					t.Fatal(err)
				}
				if !hasErrorDiagnostics(report.Diags) || !hasDiagCode(report.Diags, test.code) || report.InSync() {
					t.Fatalf("unsafe native file not refused: %+v", report)
				}
				got, _ := os.ReadFile(destination)
				if !bytes.Equal(got, []byte(original)) {
					t.Fatalf("refusal changed bytes: %s", got)
				}
				entries, _ := os.ReadDir(filepath.Dir(destination))
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".tmp") || strings.HasPrefix(entry.Name(), ".agent-marketplace-") {
						t.Fatalf("staging residue: %s", entry.Name())
					}
				}
			})
		}
	}
}
