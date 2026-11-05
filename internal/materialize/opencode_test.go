package materialize

import (
	"bytes"
	"context"
	"fmt"
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

func openCodeSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	return t.TempDir()
}
func openCodeResource(r ir.Resource) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{Entry: catalog.Entry{Name: r.Name(), Resource: r}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetOpenCode}}
}

// spec: opencode-native-configuration/mcp-update-preserves-handwritten-jsonc
// spec: opencode-native-configuration/creation-and-conversion-retain-private-modes
func TestOpenCodeMCPReconciliation(t *testing.T) {
	repo := openCodeSandbox(t)
	file := filepath.Join(repo, "opencode.jsonc")
	initial := []byte("{\n  // handwritten settings\n  \"permission\": {\"bash\": \"ask\"},\n  \"mcp\": {\n    // handwritten server\n    \"manual\": {\"type\":\"local\",\"command\":[\"manual\"]}\n  }\n}\n")
	if err := os.WriteFile(file, initial, 0600); err != nil {
		t.Fatal(err)
	}
	server := func(command string) []resolver.ResolvedArtifact {
		return []resolver.ResolvedArtifact{openCodeResource(ir.NewMcpServer("managed", command, nil, nil, nil, ir.SourceRef{}))}
	}
	for _, command := range []string{"first", "second"} {
		report, err := Run(context.Background(), repo, server(command))
		if err != nil || diag.HasErrors(report.Diags) {
			t.Fatalf("sync: %v %#v", err, report)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(`"`+command+`"`)) || !bytes.Contains(data, []byte("// handwritten server")) || !bytes.Contains(data, []byte(`"manual": {"type":"local","command":["manual"]}`)) || !bytes.Contains(data, []byte(`"permission": {"bash": "ask"}`)) || bytes.Contains(data, []byte("_managed_by")) {
			t.Fatalf("config: %s", data)
		}
	}
	before, _ := os.ReadFile(file)
	stat, _ := os.Stat(file)
	if _, err := Run(context.Background(), repo, server("second")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(file)
	repeated, _ := os.Stat(file)
	if !bytes.Equal(before, after) || !stat.ModTime().Equal(repeated.ModTime()) || repeated.Mode().Perm() != 0600 {
		t.Fatal("no-op changed bytes, mtime, or mode")
	}
}

// spec: opencode-native-configuration/missing-ownership-and-collisions-preserve-user-entries
// spec: opencode-native-configuration/read-only-inspection-creates-no-native-or-ownership-state
func TestOpenCodeReadOnlyAndUnmanagedCollision(t *testing.T) {
	repo := openCodeSandbox(t)
	resource := []resolver.ResolvedArtifact{openCodeResource(ir.NewMcpServer("managed", "server", nil, nil, nil, ir.SourceRef{}))}
	if _, err := Run(context.Background(), repo, resource, WithDryRun()); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(repo, "opencode.json"), filepath.Join(repo, ".opencode"), os.Getenv("XDG_STATE_HOME")} {
		if _, err := os.Lstat(file); !os.IsNotExist(err) {
			t.Fatalf("read-only operation created %s", filepath.Base(file))
		}
	}
	file := filepath.Join(repo, "opencode.json")
	initial := []byte(`{"mcp":{"managed":{"type":"local","command":["handwritten"]}}}`)
	if err := os.WriteFile(file, initial, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repo, resource)
	if err == nil && !diag.HasErrors(report.Diags) {
		t.Fatal("unmanaged collision succeeded")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(after, initial) {
		t.Fatal("unmanaged entry overwritten")
	}
	var codes []string
	for _, d := range report.Diags {
		codes = append(codes, d.Code)
	}
	if !strings.Contains(strings.Join(codes, ","), "ownership.unmanaged-key") {
		t.Fatalf("collision diagnostic: %v", codes)
	}
}

// spec: harness-opencode/all-selectable-resource-kinds-reach-the-native-layout
// spec: opencode-hooks/removing-one-contribution-preserves-the-others
// spec: opencode-native-configuration/native-installation-and-repetition-preserve-supported-aliases
func TestOpenCodeResourceInstallationAndHookRemoval(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			repo := openCodeSandbox(t)
			resources := []ir.Resource{
				ir.NewSkill("review", "Review", "Review\n", nil, nil, ir.SourceRef{}),
				ir.NewCommand("check", "Check", "Check $ARGUMENTS\n", nil, ir.SourceRef{}),
				ir.NewSubagent("reviewer", "Review", "Review\n", "", nil, "", nil, "", nil, ir.SourceRef{}),
				ir.NewHookWithMatcher("first", "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{}),
				ir.NewHookWithMatcher("second", "PreToolUse", "Read", "true", "command", nil, ir.SourceRef{}),
				ir.NewMcpServer("server", "server", nil, nil, nil, ir.SourceRef{}),
			}
			resolved := []resolver.ResolvedArtifact{}
			for _, resource := range resources {
				resolved = append(resolved, openCodeResource(resource))
			}
			options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
			report, err := Run(context.Background(), repo, resolved, options...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("sync: %v %#v", err, report)
			}
			for _, rel := range []string{"skills/review/SKILL.md", "commands/check.md", "agents/reviewer.md", "hooks/agent-marketplace/first.json", "hooks/agent-marketplace/second.json", "plugins/agent-marketplace.js"} {
				if _, err := os.Stat(filepath.Join(repo, ".opencode", filepath.FromSlash(rel))); err != nil {
					t.Fatalf("native output missing: %s %v", rel, err)
				}
			}
			adapter := filepath.Join(repo, ".opencode", "plugins", "agent-marketplace.js")
			info, err := os.Lstat(adapter)
			if err != nil || !info.Mode().IsRegular() {
				t.Fatal("adapter must remain at its native path for relative declaration resolution")
			}
			retained, _ := os.ReadFile(filepath.Join(repo, ".opencode", "hooks", "agent-marketplace", "second.json"))
			resolved = append(resolved[:3], resolved[4:]...)
			report, err = Run(context.Background(), repo, resolved, options...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("remove: %v %#v", err, report)
			}
			if _, err := os.Lstat(filepath.Join(repo, ".opencode", "hooks", "agent-marketplace", "first.json")); !os.IsNotExist(err) {
				t.Fatal("removed hook contribution survived")
			}
			after, _ := os.ReadFile(filepath.Join(repo, ".opencode", "hooks", "agent-marketplace", "second.json"))
			if !bytes.Equal(retained, after) {
				t.Fatal("other hook changed during removal")
			}
			if _, err := os.Stat(adapter); err != nil {
				t.Fatal("remaining hook lost its adapter")
			}
		})
	}
}

func TestOpenCodeContainedAliasesInBothModes(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(fmt.Sprintf("machine=%v/%s", machine, mode), func(t *testing.T) {
				repo := openCodeSandbox(t)
				base, alias := repo, filepath.Join(repo, ".opencode")
				if machine {
					base, alias = os.Getenv("HOME"), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
				}
				physical := filepath.Join(base, "native-resources")
				if err := os.MkdirAll(physical, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(physical, alias); err != nil {
					t.Fatal(err)
				}
				resource := openCodeResource(ir.NewCommand("check", "Check", "Body\n", nil, ir.SourceRef{}))
				if machine {
					resource.Source = config.UserPersonal
				}
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				for range 2 {
					report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resource}, options...)
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("aliased install: %v %v", err, report.Diags)
					}
				}
				if data, err := os.ReadFile(filepath.Join(physical, "commands", "check.md")); err != nil || !bytes.Contains(data, []byte("Body")) {
					t.Fatalf("native alias output: %v", err)
				}
				if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("native root alias changed")
				}
				report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resource}, append(options, WithDryRun())...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("aliased check: %v %v", err, report.Diags)
				}
				for _, root := range report.PerRoot {
					if len(root.Written) != 0 {
						t.Fatal("unchanged alias reported output drift")
					}
				}
			})
		}
	}
}
