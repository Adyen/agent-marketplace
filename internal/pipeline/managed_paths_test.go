package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// writeSourceSkill writes a minimal skill directory and returns its path.
func writeSourceSkill(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	body := "---\nname: " + name + "\ndescription: Test skill\n---\n\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return dir
}

// machineScopeResolved returns machine-scoped artifacts (skill + command +
// MCP server) for the given target.
func machineScopeResolved(t *testing.T, sourceRoot string, target hcc.Target) []resolver.ResolvedArtifact {
	t.Helper()
	skillDir := writeSourceSkill(t, sourceRoot, "managed-skill")
	irSkill := ir.NewSkill("managed-skill", "Test skill", "Body", nil, nil, ir.SourceRef{File: filepath.Join(skillDir, "SKILL.md"), Line: 1})
	irCmd := ir.NewCommand("managed-command", "Test command", "echo hi", nil, ir.SourceRef{})
	irMCP := ir.NewMcpServer("managed-mcp", "echo", nil, nil, nil, ir.SourceRef{})

	return []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindSkill, Name: "managed-skill", Path: skillDir, Resource: irSkill},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{target},
		},
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "managed-command", Resource: irCmd},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{target},
		},
		{
			Entry:     catalog.Entry{Kind: ir.KindMcpServer, Name: "managed-mcp", Resource: irMCP},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{target},
		},
	}
}

// TestCollectManagedPathsCoversMachineScopeOutputs is a regression test for the
// machine-scope Windsurf backup path divergence: materialization writes
// machine-wide workflows under <machine root>/global_workflows, while
// CollectManagedPaths used to derive relative paths from the repo-scope
// component list (which contains "workflows"). Any top-level path that
// machine-scope materialization actually writes must be covered by the managed
// (backed up) path set.
func TestCollectManagedPathsCoversMachineScopeOutputs(t *testing.T) {
	for _, target := range []hcc.Target{
		hcc.TargetWindsurf,
		hcc.TargetClaude,
		hcc.TargetFactory,
		hcc.TargetCursor,
		hcc.TargetGemini,
	} {
		t.Run(string(target), func(t *testing.T) {
			repoRoot := t.TempDir()
			homeDir := t.TempDir()
			sourceRoot := t.TempDir()
			t.Setenv("HOME", homeDir)

			resolved := machineScopeResolved(t, sourceRoot, target)

			if _, err := materialize.Run(context.Background(), repoRoot, resolved); err != nil {
				t.Fatalf("materialize.Run: %v", err)
			}

			managed := CollectManagedPaths(resolved)
			mp, ok := managed[target]
			if !ok {
				t.Fatalf("CollectManagedPaths has no entry for %s", target)
			}

			machineRoot, ok := harness.MachineRoot(target)
			if !ok {
				t.Fatalf("no machine root for %s", target)
			}
			entries, err := os.ReadDir(machineRoot)
			if err != nil {
				t.Fatalf("read machine root %s: %v", machineRoot, err)
			}
			if len(entries) == 0 {
				t.Fatalf("machine root %s is empty; materialization wrote nothing", machineRoot)
			}
			for _, entry := range entries {
				if !slices.Contains(mp.RelPaths, entry.Name()) {
					t.Errorf("machine-scope output %q under %s is not covered by managed paths %v",
						entry.Name(), machineRoot, mp.RelPaths)
				}
			}
		})
	}
}

// TestCollectManagedPathsWindsurfMachineComponents pins the exact machine-scope
// Windsurf relative paths: global_workflows (not workflows), skills, rules, and
// hooks at the Windsurf machine root, plus the managed root config file.
func TestCollectManagedPathsWindsurfMachineComponents(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	resolved := []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Kind: ir.KindCommand, Name: "managed-command", Resource: ir.NewCommand("managed-command", "Test", "echo", nil, ir.SourceRef{})},
			Source:    config.UserPersonal,
			Harnesses: []hcc.Target{hcc.TargetWindsurf},
		},
	}

	managed := CollectManagedPaths(resolved)
	mp, ok := managed[hcc.TargetWindsurf]
	if !ok {
		t.Fatal("CollectManagedPaths has no entry for windsurf")
	}
	want := []string{"skills", "global_workflows", "hooks", "rules", "mcp_config.json"}
	if !slices.Equal(mp.RelPaths, want) {
		t.Errorf("windsurf machine-scope managed rel paths = %v, want %v", mp.RelPaths, want)
	}
}

func TestCollectManagedPathsExcludesMachineDevinRules(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	managed := CollectManagedPaths(machineScopeSkill(hcc.TargetDevin))
	mp, ok := managed[hcc.TargetDevin]
	if !ok {
		t.Fatal("CollectManagedPaths has no entry for devin")
	}
	if slices.Contains(mp.RelPaths, "rules") {
		t.Fatalf("machine Devin rules directory must not be backed up: %v", mp.RelPaths)
	}
	for _, want := range []string{"skills", "agents"} {
		if !slices.Contains(mp.RelPaths, want) {
			t.Errorf("machine Devin sibling component %q missing from managed paths: %v", want, mp.RelPaths)
		}
	}
}

// declaredTargets returns every target with TargetInfo, so a new target is
// covered by the aggregate-backup tests below without being listed here.
func declaredTargets(t *testing.T) []hcc.Target {
	t.Helper()
	if err := harness.TargetInfoError(); err != nil {
		t.Fatalf("harness.TargetInfoError: %v", err)
	}
	targets := make([]hcc.Target, 0, len(harness.TargetInfoMap))
	for target := range harness.TargetInfoMap {
		if target == hcc.Target(targetmeta.TargetCodex) {
			continue // Codex is covered by the split-root backup-set tests.
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	if len(targets) == 0 {
		t.Fatal("harness.TargetInfoMap is empty; these tests would be vacuous")
	}
	return targets
}

func TestCollectCodexBackupPartitionsUsesBothNativeRoots(t *testing.T) {
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	partitions, err := CollectCodexBackupPartitions(machineScopeSkill(hcc.Target(targetmeta.TargetCodex)))
	if err != nil {
		t.Fatal(err)
	}
	canonicalCodexHome, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"machine-agents": filepath.Join(canonicalHome, ".agents"), "machine-codex": canonicalCodexHome}
	if len(partitions) != len(want) {
		t.Fatalf("partitions = %#v", partitions)
	}
	for _, partition := range partitions {
		if partition.Root != want[partition.ID] {
			t.Errorf("partition = %#v, want root %q", partition, want[partition.ID])
		}
	}
}

func TestCodexBackupPartitionsContainOnlyEvidencedManagedPaths(t *testing.T) {
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	personalSkill := filepath.Join(home, ".agents", "skills", "personal")
	personalAgent := filepath.Join(codexHome, "agents", "personal.toml")
	if err := os.MkdirAll(personalSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(personalAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personalAgent, []byte("personal"), 0o644); err != nil {
		t.Fatal(err)
	}
	managedSource := filepath.Join(cache.CacheDir(), "registry", "managed")
	if err := os.MkdirAll(managedSource, 0o755); err != nil {
		t.Fatal(err)
	}
	managedLink := filepath.Join(home, ".agents", "skills", "managed")
	if err := os.Symlink(managedSource, managedLink); err != nil {
		t.Fatal(err)
	}
	partitions, err := CodexBackupPartitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, partition := range partitions {
		switch partition.ID {
		case "machine-agents":
			if !slices.Equal(partition.RelPaths, []string{"skills/managed"}) {
				t.Fatalf("machine-agents paths = %v", partition.RelPaths)
			}
		case "machine-codex":
			if len(partition.RelPaths) != 0 {
				t.Fatalf("machine-codex paths = %v", partition.RelPaths)
			}
		}
	}
	beforeRemoval, err := CollectCodexBackupPartitions(nil)
	if err != nil || len(beforeRemoval) != 2 {
		t.Fatalf("existing managed output was not backed up before removal: %#v, %v", beforeRemoval, err)
	}
}

// machineScopeSkill returns a single machine-scope resolved artifact, which is
// all CollectManagedPaths needs to include a target in its result.
func machineScopeSkill(target hcc.Target) []resolver.ResolvedArtifact {
	return []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Kind: ir.KindSkill, Name: "managed-skill", Resource: ir.NewSkill("managed-skill", "Test", "Body", nil, nil, ir.SourceRef{})},
		Source:    config.UserPersonal,
		Harnesses: []hcc.Target{target},
	}}
}

// TestCollectManagedPathsCoversEveryDeclaredAggregate is the backup-coverage
// half of bead agent-marketplace-5exj.27. Every machine-scope file a target
// merges into must be recoverable, either because the collector names it
// directly (Gemini's settings.json, which sits at the Gemini machine root and
// belongs to no component directory) or because it lives inside a component
// directory that is archived whole (Claude's hooks/hooks.json inside hooks/).
//
// The expectation is the declared aggregate set, which
// materialize.TestAggregateFilesMatchMaterializedMergeArtifacts independently
// pins against what the compilers really emit. The two together are the proof
// that every MUTATED aggregate is backed up.
func TestCollectManagedPathsCoversEveryDeclaredAggregate(t *testing.T) {
	for _, target := range declaredTargets(t) {
		t.Run(string(target), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			managed := CollectManagedPaths(machineScopeSkill(target))
			mp, ok := managed[target]
			if !ok {
				t.Fatalf("CollectManagedPaths has no entry for %s", target)
			}

			aggregates := expectedMachineRootAggregates(target)
			if len(aggregates) == 0 {
				t.Fatalf("%s declares no machine-scope aggregate files; every current target merges into at least one", target)
			}
			for _, rel := range aggregates {
				if !coveredByAncestor(rel, mp.RelPaths) {
					t.Errorf("machine-scope aggregate %q of %s is MUTATED WITH NO SAFETY BACKUP: managed paths are %v", rel, target, mp.RelPaths)
				}
			}
			for i, rel := range mp.RelPaths {
				if coveredByAncestor(rel, mp.RelPaths[:i]) {
					t.Errorf("%s managed path %q is already covered by an earlier entry in %v; it would be archived twice", target, rel, mp.RelPaths)
				}
			}
		})
	}
}

// TestDeclaredAggregatesAreRecoverableFromBackup proves the coverage claim
// against the real archive rather than against the path list: a handwritten
// aggregate file is destroyed after the backup is taken and must come back
// byte-identical from backup.Restore.
//
// Before this bead, the Gemini subtest failed — settings.json is not inside any
// Gemini component directory and was not declared, so nothing archived it while
// every hook sync merged into it.
func TestDeclaredAggregatesAreRecoverableFromBackup(t *testing.T) {
	for _, target := range declaredTargets(t) {
		t.Run(string(target), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			machineRoot, ok := harness.MachineRoot(target)
			if !ok {
				t.Fatalf("no machine root for %s", target)
			}
			aggregates := expectedMachineRootAggregates(target)
			handwritten := make(map[string]string, len(aggregates))
			for _, rel := range aggregates {
				full := filepath.Join(machineRoot, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir for %s: %v", rel, err)
				}
				content := "{\n  // handwritten " + rel + "\n}\n"
				if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", full, err)
				}
				handwritten[rel] = content
			}

			if err := backup.Create(CollectManagedPaths(machineScopeSkill(target))); err != nil {
				t.Fatalf("backup.Create: %v", err)
			}
			for rel := range handwritten {
				if err := os.Remove(filepath.Join(machineRoot, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("remove %s: %v", rel, err)
				}
			}
			if _, err := backup.Restore(target, machineRoot); err != nil {
				t.Fatalf("backup.Restore(%s): %v", target, err)
			}
			for rel, want := range handwritten {
				got, err := os.ReadFile(filepath.Join(machineRoot, filepath.FromSlash(rel)))
				if err != nil {
					t.Errorf("aggregate %q of %s was not recovered from the safety backup: %v", rel, target, err)
					continue
				}
				if string(got) != want {
					t.Errorf("aggregate %q of %s restored as %q, want %q", rel, target, got, want)
				}
			}
		})
	}
}
