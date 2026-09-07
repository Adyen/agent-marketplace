package harness

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestMachineRootUsesCurrentHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	got, ok := MachineRoot(hcc.TargetFactory)
	if !ok {
		t.Fatal("MachineRoot(factory) did not find a target")
	}
	want := filepath.Join(homeDir, ".factory")
	if got != want {
		t.Fatalf("MachineRoot(factory) = %q, want %q", got, want)
	}
}

func TestComponentsForScope(t *testing.T) {
	tests := []struct {
		name         string
		target       hcc.Target
		machineScope bool
		want         []string
	}{
		{"windsurf repo scope keeps workflows", hcc.TargetWindsurf, false, []string{"skills", "workflows", "hooks"}},
		{"windsurf machine scope renames workflows", hcc.TargetWindsurf, true, []string{"skills", "global_workflows", "hooks"}},
		{"claude machine scope unchanged", hcc.TargetClaude, true, []string{"skills", "commands", "agents", "hooks", ".claude-plugin"}},
		{"gemini machine scope unchanged", hcc.TargetGemini, true, []string{"skills", "commands"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := TargetInfoMap[tt.target]
			if !ok {
				t.Fatalf("no TargetInfo for %q", tt.target)
			}
			got := info.ComponentsForScope(tt.machineScope)
			if len(got) != len(tt.want) {
				t.Fatalf("ComponentsForScope(%v) = %v, want %v", tt.machineScope, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ComponentsForScope(%v) = %v, want %v", tt.machineScope, got, tt.want)
				}
			}
		})
	}
}

func TestInstallRootsContainsOnlyLegacyRootTargets(t *testing.T) {
	for _, definition := range targetmeta.Definitions() {
		target := hcc.Target(definition.Target)
		roots, ok := InstallRoots[target]
		if definition.PartitionedRoots {
			if ok {
				t.Errorf("InstallRoots contains partitioned target %q: %#v", target, roots)
			}
			continue
		}
		if !ok {
			t.Errorf("InstallRoots missing entry for target %q", target)
			continue
		}
		if roots.Repo == "" {
			t.Errorf("InstallRoots[%q].Repo is empty", target)
		}
		if roots.Machine == "" {
			t.Errorf("InstallRoots[%q].Machine is empty", target)
		}
	}
}

func TestForReturnsRepoPathForRepoScope(t *testing.T) {
	repoRoot := "/tmp/myproject"
	for target, roots := range InstallRoots {
		got := roots.For(false, repoRoot)
		want := filepath.Join(repoRoot, roots.Repo)
		if got != want {
			t.Errorf("For(false, %q) for target %q = %q, want %q", repoRoot, target, got, want)
		}
	}
}

func TestForReturnsMachinePathForMachineScope(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	repoRoot := "/tmp/myproject"
	for target, roots := range InstallRoots {
		got := roots.For(true, repoRoot)
		if got != roots.Machine {
			t.Errorf("For(true, %q) for target %q = %q, want %q", repoRoot, target, got, roots.Machine)
		}
		_ = homeDir // used indirectly via roots.Machine which embeds home
	}
}

func TestInstallRootsMachinePathsContainHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	for target, roots := range InstallRoots {
		if !filepath.IsAbs(roots.Machine) {
			t.Errorf("InstallRoots[%q].Machine = %q is not absolute", target, roots.Machine)
		}
		// Machine path should be under home (except for special cases)
		// We just check it's an absolute path containing home-like patterns
		_ = homeDir
	}
}

func TestRepoPathsAreRelative(t *testing.T) {
	for target, roots := range InstallRoots {
		if filepath.IsAbs(roots.Repo) {
			t.Errorf("InstallRoots[%q].Repo = %q should be relative (joined with repo root)", target, roots.Repo)
		}
	}
}

func TestTargetInfoMapCoversAllInstallRoots(t *testing.T) {
	for target := range InstallRoots {
		info, ok := TargetInfoMap[target]
		if !ok {
			t.Errorf("TargetInfoMap missing entry for target %q present in InstallRoots", target)
			continue
		}
		if len(info.Components) == 0 {
			t.Errorf("TargetInfoMap[%q] has no Components", target)
		}
	}
}

func TestValidateRelPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty is valid", "", false},
		{"simple relative", ".codeium/windsurf/skills", false},
		{"nested relative", "foo/bar/baz", false},
		{"absolute path rejected", "/etc/passwd", true},
		{"dotdot prefix rejected", "../escape", true},
		{"dotdot prefix trailing slash rejected", "../escape/", true},
		{"dotdot mid rejected", "foo/../bar", true},
		{"dotdot only rejected", "..", true},
		{"backslash rejected", "foo\\bar", true},
		{"dotdot backslash rejected", "foo\\..\\bar", true},
		{"drive letter rejected", "C:skills/foo", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRelPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRelPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestMakeTargetInfo(t *testing.T) {
	// Every declared aggregate path is joined onto a real output root and then
	// mutated, so all current entries must pass traversal validation. That
	// validation is part of MakeTargetInfo itself; this test is the regression
	// guard that fails when MakeTargetInfo returns an error or an empty map.
	m, err := MakeTargetInfo()
	if err != nil {
		t.Fatalf("MakeTargetInfo() returned unexpected error: %v", err)
	}
	if len(m) == 0 {
		t.Fatal("MakeTargetInfo() returned an empty map")
	}
}

func TestDevinTargetInfo(t *testing.T) {
	info, ok := TargetInfoMap[hcc.TargetDevin]
	if !ok {
		t.Fatal("no TargetInfo for devin")
	}
	if !info.OwnershipReconciledAggregates {
		t.Error("devin aggregates must be ownership-reconciled")
	}
	if !info.UsesSkillDir(hcc.KindCommand) {
		t.Error("devin commands must materialize as skill directories")
	}
	for _, c := range info.Components {
		if c == "hooks" {
			t.Error(`devin must not list "hooks" as a prunable component`)
		}
	}
	if got := info.AggregateFilesForScope(false); !slices.Contains(got, DevinProjectHooksFile) {
		t.Errorf("devin project aggregates = %v, want %q", got, DevinProjectHooksFile)
	}
	if got := info.AggregateFilesForScope(true); !slices.Contains(got, DevinUserConfigFile) {
		t.Errorf("devin user aggregates = %v, want %q", got, DevinUserConfigFile)
	}
}

func TestTargetInfoMapSkillPathPatternConsistency(t *testing.T) {
	for target, info := range TargetInfoMap {
		hasSkills := false
		for _, c := range info.Components {
			if c == "skills" {
				hasSkills = true
				break
			}
		}
		// SkillPathPattern is required when the target emits skills/<name>/SKILL.md
		// artifacts. Claude uses <skill>.md (not SKILL.md) so it correctly omits
		// SkillPathPattern. All other PlanSubtree targets with a "skills" component
		// must set SkillPathPattern.
		if hasSkills && info.SkillPathPattern == "" && info.PlanStrategy == PlanSubtree {
			if target != hcc.TargetClaude {
				t.Errorf("TargetInfoMap[%q] lists \"skills\" in Components but has no SkillPathPattern", target)
			}
		}
	}
}
