package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: opencode-native-configuration/effective-native-identity-collisions-do-not-shadow-handwritten-agents
func TestOpenCodeConfigAndLegacyIdentityCollisions(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
		for _, kind := range []string{"command", "agent"} {
			for _, declaration := range []string{"config", "legacy"} {
				t.Run(string(mode)+"/"+kind+"/"+declaration, func(t *testing.T) {
					repo := openCodeSandbox(t)
					manual := filepath.Join(repo, "opencode.json")
					content := []byte(`{"command":{"manual":{"description":"Handwritten","template":"Body"}}}`)
					if kind == "agent" {
						content = []byte(`{"agent":{"manual":{"description":"Handwritten","mode":"subagent","prompt":"Body"}}}`)
					}
					if declaration == "legacy" {
						manual = filepath.Join(repo, ".opencode", kind, "manual.md")
						content = []byte("---\ndescription: Handwritten\nmode: subagent\n---\nManual body\n")
					}
					additiveSafetyWrite(t, manual, string(content))
					before, err := os.Stat(manual)
					if err != nil {
						t.Fatal(err)
					}
					resource := ir.Resource(ir.NewCommand("manual", "Selected", "Changed", nil, ir.SourceRef{}))
					if kind == "agent" {
						resource = ir.NewSubagent("manual", "Selected", "Changed", "", nil, "", nil, "", nil, ir.SourceRef{})
					}
					selected := []resolver.ResolvedArtifact{openCodeResource(resource), openCodeResource(ir.NewCommand("control", "", "Independent", nil, ir.SourceRef{}))}
					report, err := Run(t.Context(), repo, selected, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
					if err != nil || !diag.HasCode(report.Diags, "materialize.collision.user-owned") {
						t.Fatalf("handwritten identity was shadowed: %v %v", err, report.Diags)
					}
					after, err := os.ReadFile(manual)
					info, statErr := os.Stat(manual)
					if err != nil || statErr != nil || !bytes.Equal(after, content) || !info.ModTime().Equal(before.ModTime()) || info.Mode() != before.Mode() {
						t.Fatal("collision changed handwritten bytes, mode, or mtime")
					}
					component := "commands"
					if kind == "agent" {
						component = "agents"
					}
					if _, err := os.Lstat(filepath.Join(repo, ".opencode", component, "manual.md")); !os.IsNotExist(err) {
						t.Fatal("colliding definition was published")
					}
					if _, err := os.Stat(filepath.Join(repo, ".opencode", "commands", "control.md")); err != nil {
						t.Fatal("independent valid command was blocked")
					}
				})
			}
		}
	}
}

func TestOpenCodeCollisionDiagnosticsStayScopeLocal(t *testing.T) {
	repo := openCodeSandbox(t)
	additiveSafetyWrite(t, filepath.Join(repo, "opencode.json"), `{"mcp":{"manual":{"type":"local","command":["handwritten"]}}}`)
	project := openCodeResource(ir.NewMcpServer("manual", "selected", nil, nil, nil, ir.SourceRef{}))
	machine := openCodeResource(ir.NewMcpServer("control", "independent", nil, nil, nil, ir.SourceRef{}))
	machine.Source = config.UserPersonal
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{project, machine})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == diag.CodeMaterializeOpenCodeAggregateCollision {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one scoped collision produced %d OpenCode collision diagnostics", count)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json")); err != nil {
		t.Fatal("independent machine aggregate did not publish")
	}
}

// spec: opencode-native-configuration/unsafe-identity-inventory-refuses-potentially-shadowed-definitions
func TestOpenCodeIdentityInventoryFailsClosed(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
		for _, input := range []string{"malformed", "oversized", "contained-alias", "dangling-alias", "hard-link"} {
			t.Run(string(mode)+"/"+input, func(t *testing.T) {
				repo := openCodeSandbox(t)
				configuration := filepath.Join(repo, ".opencode", "opencode.json")
				content := `{"command":{"manual":{"template":"Handwritten"}}}`
				wantCode := diag.CodeMaterializeOpenCodeAggregateMalformed
				switch input {
				case "malformed":
					content = `{"command":`
					additiveSafetyWrite(t, configuration, content)
				case "oversized":
					additiveSafetyWrite(t, configuration, content)
					file, err := os.OpenFile(configuration, os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					if err := file.Truncate((64 << 20) + 1); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				case "contained-alias":
					target := filepath.Join(repo, ".opencode", "manual.json")
					additiveSafetyWrite(t, target, content)
					if err := os.Symlink("manual.json", configuration); err != nil {
						t.Fatal(err)
					}
					wantCode = "materialize.collision.user-owned"
				case "dangling-alias":
					if err := os.MkdirAll(filepath.Dir(configuration), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("missing.json", configuration); err != nil {
						t.Fatal(err)
					}
				case "hard-link":
					target := filepath.Join(repo, ".opencode", "manual.json")
					additiveSafetyWrite(t, target, content)
					if err := os.Link(target, configuration); err != nil {
						t.Fatal(err)
					}
				}
				if input == "oversized" {
					if _, err := readOpenCodeIdentityConfig(repo, configuration); !errors.Is(err, fsboundary.ErrTooLarge) {
						t.Fatalf("oversized inventory error = %v, want size boundary", err)
					}
				}
				before, err := os.Lstat(configuration)
				if err != nil {
					t.Fatal(err)
				}
				selected := []resolver.ResolvedArtifact{
					openCodeResource(ir.NewCommand("manual", "", "Selected", nil, ir.SourceRef{})),
					openCodeResource(ir.NewSkill("control", "", "Independent", nil, nil, ir.SourceRef{})),
				}
				report, err := Run(t.Context(), repo, selected, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
				if err != nil || !diag.HasCode(report.Diags, wantCode) {
					t.Fatalf("unsafe identity inventory did not refuse: %v %v", err, report.Diags)
				}
				if _, err := os.Lstat(filepath.Join(repo, ".opencode", "commands", "manual.md")); !os.IsNotExist(err) {
					t.Fatal("unknown or colliding native identity was published")
				}
				if _, err := os.Stat(filepath.Join(repo, ".opencode", "skills", "control", "SKILL.md")); err != nil {
					t.Fatal("independent skill was blocked")
				}
				after, err := os.Lstat(configuration)
				if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatal("identity refusal changed configuration identity, size, mode, or mtime")
				}
			})
		}
	}
}

// spec: opencode-hooks/synchronization-rejects-excess-declarations-in-either-mode
func TestOpenCodeSyncHookInvocationBudget(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
		for _, machine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine-%v", mode, machine), func(t *testing.T) {
				repo := openCodeSandbox(t)
				root := filepath.Join(repo, ".opencode")
				hookResource := func(name string) resolver.ResolvedArtifact {
					selected := openCodeResource(ir.NewHookWithMatcher(name, "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{}))
					if machine {
						selected.Source = config.UserPersonal
					}
					return selected
				}
				if machine {
					root = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
				}
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				selected := make([]resolver.ResolvedArtifact, 0, 66)
				for n := range 64 {
					selected = append(selected, hookResource(fmt.Sprintf("check-%02d", n)))
				}
				report, err := Run(t.Context(), repo, selected, options...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("64-handler control: %v %v", err, report.Diags)
				}
				file := filepath.Join(root, "hooks", "agent-marketplace", "check-00.json")
				prior, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				selected = append(selected,
					hookResource("check-64"),
					openCodeResource(ir.NewCommand("control", "", "Independent", nil, ir.SourceRef{})),
				)
				check, err := Run(t.Context(), repo, selected, append(options, WithDryRun())...)
				if err != nil || !diag.HasCode(check.Diags, diag.CodeHookOpenCodeUnsupported) {
					t.Fatalf("check accepted an unusable hook generation: %v %v", err, check.Diags)
				}
				if _, err := os.Stat(filepath.Join(repo, ".opencode", "commands", "control.md")); !os.IsNotExist(err) {
					t.Fatal("check published an independent command")
				}
				report, err = Run(t.Context(), repo, selected, options...)
				if err != nil || !diag.HasCode(report.Diags, diag.CodeHookOpenCodeUnsupported) {
					t.Fatalf("sync accepted an unusable hook generation: %v %v", err, report.Diags)
				}
				if _, err := os.Lstat(filepath.Join(root, "hooks", "agent-marketplace", "check-64.json")); !os.IsNotExist(err) {
					t.Fatal("over-budget declaration was published")
				}
				after, err := os.ReadFile(file)
				info, statErr := os.Stat(file)
				if err != nil || statErr != nil || !bytes.Equal(after, prior) || info.Mode() != before.Mode() || !info.ModTime().Equal(before.ModTime()) {
					t.Fatal("budget refusal changed installed hook bytes, mode, or mtime")
				}
				if _, err := os.Stat(filepath.Join(repo, ".opencode", "commands", "control.md")); err != nil {
					t.Fatal("budget refusal blocked an independent command")
				}
			})
		}
	}
}

// spec: opencode-hooks/required-hook-selections-refuse-unenforceable-activation
func TestOpenCodeRequiredHookCannotClaimInactiveEnforcement(t *testing.T) {
	for _, installed := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("installed-%v/check-%v", installed, dryRun), func(t *testing.T) {
				repo := openCodeSandbox(t)
				hook := ir.NewHookWithMatcher("required-check", "PreToolUse", "Bash", "true", "command", nil, ir.SourceRef{})
				selected := openCodeResource(hook)
				selected.Source = config.UserPersonal
				file := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "hooks", "agent-marketplace", "required-check.json")
				var prior []byte
				if installed {
					report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected})
					if err != nil || diag.HasErrors(report.Diags) {
						t.Fatalf("non-required active-hook control: %v %v", err, report.Diags)
					}
					prior, err = os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
				}
				selected.Required = true
				hook.Command = "changed"
				control := openCodeResource(ir.NewCommand("control", "", "Independent", nil, ir.SourceRef{}))
				var options []RunOption
				if dryRun {
					options = append(options, WithDryRun())
				}
				report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected, control}, options...)
				if err != nil || !diag.HasCode(report.Diags, diag.CodeHookOpenCodeUnsupported) {
					t.Fatalf("unenforceable required hook was claimed as supported: %v %v", err, report.Diags)
				}
				after, readErr := os.ReadFile(file)
				if installed && (readErr != nil || !bytes.Equal(after, prior)) {
					t.Fatal("required-hook refusal changed installed declaration")
				}
				if !installed && !os.IsNotExist(readErr) {
					t.Fatal("required-hook refusal published a declaration")
				}
				if !dryRun {
					if _, err := os.Stat(filepath.Join(repo, ".opencode", "commands", "control.md")); err != nil {
						t.Fatal("independent supported resource was blocked")
					}
				}
			})
		}
	}
}
