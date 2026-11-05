package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func cleanupLinkSkill(t *testing.T, name string, machine bool, targets ...hcc.Target) resolver.ResolvedArtifact {
	resource := devinResolved(t, name, ir.NewSkill(name, "skill", name+" original", nil, nil, ir.SourceRef{}), machine)
	resource.Kind = ir.KindSkill
	resource.Harnesses = targets
	resource.RegistryURL = "https://example.invalid/registry.git"
	resource.Ref = "main"
	resource.CatalogKey = "locked-catalog"
	return resource
}

func seedCleanupLinks(t *testing.T, repo string, resources ...resolver.ResolvedArtifact) {
	t.Helper()
	report, err := Run(context.Background(), repo, resources)
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v error=%v", report, err)
	}
}

func TestManagedResourceLinkPlanProtectsOtherKinds(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			repo, home := devinEnv(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			command := cleanupLinkSkill(t, "shared", false, target)
			command.Kind = ir.KindCommand
			command.Resource = ir.NewCommand("shared", "command", "body", nil, ir.SourceRef{})
			seedCleanupLinks(t, repo, command)
			remaining := cleanupLinkSkill(t, "shared", false, target)
			if _, err := PrepareManagedLinkRemoval(context.Background(), repo, command, []resolver.ResolvedArtifact{remaining}, config.DefaultMaterializationModes()); !errors.Is(err, ErrManagedLinkCleanupConflict) {
				t.Fatalf("shared path error=%v", err)
			}
			root := ".devin"
			if target == hcc.TargetCodex {
				root = ".agents"
			}
			if _, err := os.Stat(filepath.Join(repo, root, "skills", "shared", "SKILL.md")); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("distinct subagent path", func(t *testing.T) {
		repo, home := devinEnv(t)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		subagent := cleanupLinkSkill(t, "shared", false, hcc.TargetDevin)
		subagent.Kind = ir.KindSubagent
		subagent.Resource = ir.NewSubagent("shared", "agent", "body", "", nil, "", nil, "", nil, ir.SourceRef{})
		skill := cleanupLinkSkill(t, "shared", false, hcc.TargetDevin)
		seedCleanupLinks(t, repo, subagent, skill)
		skillPath := filepath.Join(repo, ".devin", "skills", "shared", "SKILL.md")
		before := readFileString(t, skillPath)
		plan, err := PrepareManagedLinkRemoval(context.Background(), repo, subagent, []resolver.ResolvedArtifact{skill}, config.DefaultMaterializationModes())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { return dependencytxn.Result{}, nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(repo, ".devin", "agents", "shared.md")); !os.IsNotExist(err) {
			t.Fatalf("subagent remains: %v", err)
		}
		if got := readFileString(t, skillPath); got != before {
			t.Fatal("other-kind output changed")
		}
	})
}

func TestManagedSkillLinkPlanMultiTarget(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	target := cleanupLinkSkill(t, "remove", false, hcc.TargetFactory, hcc.TargetDevin)
	keep := cleanupLinkSkill(t, "keep", false, hcc.TargetFactory, hcc.TargetDevin)
	seedCleanupLinks(t, repo, target, keep)
	plan, err := PrepareManagedLinkRemoval(context.Background(), repo, target, []resolver.ResolvedArtifact{keep}, config.DefaultMaterializationModes())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) {
		calls++
		return dependencytxn.Result{Changed: []string{"published"}}, nil
	})
	if err != nil || calls != 1 || len(result.Changed) != 1 {
		t.Fatalf("calls=%d result=%#v error=%v", calls, result, err)
	}
	for _, dir := range []string{".factory", ".devin"} {
		if _, err := os.Lstat(filepath.Join(repo, dir, "skills", "remove")); !os.IsNotExist(err) {
			t.Fatalf("target remains: %v", err)
		}
		if _, err := os.Stat(filepath.Join(repo, dir, "skills", "keep", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedSkillLinkPlanRefusals(t *testing.T) {
	for _, scenario := range []string{"publish", "changed target", "still selected", "same slot command", "file mode", "shared fragment"} {
		t.Run(scenario, func(t *testing.T) {
			repo, home := devinEnv(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			target := cleanupLinkSkill(t, "remove", false, hcc.TargetDevin)
			if scenario == "shared fragment" {
				target.Harnesses = []hcc.Target{hcc.TargetDevin, hcc.TargetClaude}
			}
			seedCleanupLinks(t, repo, target)
			path := filepath.Join(repo, ".devin", "skills", "remove")
			original, err := os.Readlink(path)
			if err != nil {
				t.Fatal(err)
			}
			var remaining []resolver.ResolvedArtifact
			modes := config.DefaultMaterializationModes()
			want := ErrManagedLinkCleanupConflict
			switch scenario {
			case "still selected":
				remaining = []resolver.ResolvedArtifact{target}
			case "same slot command":
				command := devinResolved(t, "remove", ir.NewCommand("remove", "command", "body", nil, ir.SourceRef{}), false)
				command.Kind = ir.KindCommand
				remaining = []resolver.ResolvedArtifact{command}
			case "file mode":
				modes.Project = config.MaterializationModeFile
				want = ErrOwnedCleanupUnsupported
			case "shared fragment":
				want = ErrOwnedCleanupUnsupported
			}
			plan, err := PrepareManagedLinkRemoval(context.Background(), repo, target, remaining, modes)
			if scenario != "publish" && scenario != "changed target" {
				if !errors.Is(err, want) {
					t.Fatalf("preparation error=%v want=%v", err, want)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				sentinel := errors.New("publication refused")
				calls := 0
				if scenario == "changed target" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				_, err = plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) { calls++; return dependencytxn.Result{}, sentinel })
				if scenario == "publish" {
					if calls != 1 || !errors.Is(err, sentinel) {
						t.Fatalf("calls=%d err=%v", calls, err)
					}
				} else {
					if calls != 0 || !errors.Is(err, ErrManagedLinkCleanupConflict) {
						t.Fatalf("calls=%d err=%v", calls, err)
					}
					if got := readFileString(t, path); got != "foreign" {
						t.Fatalf("foreign bytes=%q", got)
					}
					return
				}
			}
			if got, err := os.Readlink(path); err != nil || got != original {
				t.Fatalf("refusal changed link: %q %v", got, err)
			}
		})
	}
}

func TestManagedSkillLinkPlanProtectsAliasedScope(t *testing.T) {
	_, home := devinEnv(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	repo := home
	target := cleanupLinkSkill(t, "remove", false, hcc.TargetCodex)
	seedCleanupLinks(t, repo, target)
	machine := cleanupLinkSkill(t, "remove", true, hcc.TargetCodex)
	if _, err := PrepareManagedLinkRemoval(context.Background(), repo, target, []resolver.ResolvedArtifact{machine}, config.DefaultMaterializationModes()); !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("aliased scope error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "remove", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestManagedSkillLinkPlanRechecksAfterPublication(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	target := cleanupLinkSkill(t, "remove", false, hcc.TargetDevin)
	seedCleanupLinks(t, repo, target)
	plan, err := PrepareManagedLinkRemoval(context.Background(), repo, target, nil, config.DefaultMaterializationModes())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".devin", "skills", "remove")
	calls := 0
	result, err := plan.CommitScope(context.Background(), func() (dependencytxn.Result, error) {
		calls++
		if err := os.Remove(path); err != nil {
			return dependencytxn.Result{}, err
		}
		if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
			return dependencytxn.Result{}, err
		}
		return dependencytxn.Result{Changed: []string{"published"}}, nil
	})
	if calls != 1 || len(result.Changed) != 1 || !errors.Is(err, ErrManagedLinkCleanupConflict) {
		t.Fatalf("calls=%d result=%#v err=%v", calls, result, err)
	}
	if got := readFileString(t, path); got != "foreign" {
		t.Fatalf("foreign bytes=%q", got)
	}
}
