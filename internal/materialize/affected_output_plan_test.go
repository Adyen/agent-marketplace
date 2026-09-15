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
)

func TestBuildPlanRejectsInvalidMaterializationRootsBeforeWrites(t *testing.T) {
	parent := t.TempDir()
	for _, test := range []struct {
		name string
		root string
		want string
	}{
		{name: "relative", root: "relative-root", want: "must be absolute"},
		{name: "missing", root: filepath.Join(parent, "missing-root"), want: "does not exist"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildPlan(context.Background(), test.root, nil); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildPlan(%q) error = %v, want %q", test.root, err, test.want)
			}
		})
	}
}

func TestBuildPlanRejectsRegularOutputRootWithoutWriting(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	root := filepath.Join(repoRoot, ".claude")
	before := []byte("user-owned output root")
	if err := os.WriteFile(root, before, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPlan(context.Background(), repoRoot, nil); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("BuildPlan error = %v, want regular output-root error", err)
	}
	got, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("regular output root changed: got %q, want %q", got, before)
	}
}

func TestPlanExecuteRefusesEnvironmentDestinationDriftBeforeWrites(t *testing.T) {
	for _, test := range []struct {
		name   string
		target hcc.Target
		mutate func(t *testing.T, home, alternate string)
	}{
		{
			name:   "home",
			target: hcc.TargetClaude,
			mutate: func(t *testing.T, _, alternate string) { t.Setenv("HOME", alternate) },
		},
		{
			name:   "codex-home",
			target: hcc.TargetCodex,
			mutate: func(t *testing.T, _, alternate string) { t.Setenv("CODEX_HOME", alternate) },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			home := t.TempDir()
			alternate := t.TempDir()
			codexHome := filepath.Join(home, "codex-home")
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", codexHome)
			resolved := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "drift", Resource: ir.NewSkill("drift", "drift", "body", nil, nil, ir.SourceRef{})},
				Source:    config.UserPersonal,
				Harnesses: []hcc.Target{test.target},
			}}
			plannedPaths := plannedMachineDestinationPaths(test.target, home, codexHome)
			seedPlannedMachineDestinations(t, test.target, home, codexHome)
			plan, err := BuildPlan(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			alternatePaths := []string{
				filepath.Join(alternate, ".claude"),
				filepath.Join(alternate, ".claude.json"),
				filepath.Join(alternate, ".agents"),
				filepath.Join(alternate, ".codex"),
			}
			plannedBefore := snapshotMaterializationPaths(t, plannedPaths)
			alternateBefore := snapshotMaterializationPaths(t, alternatePaths)

			test.mutate(t, home, alternate)
			report, err := plan.Execute(context.Background())
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !hasDiagCode(report.Diags, "materialize.plan.drift") {
				t.Fatalf("missing plan drift diagnostic: %#v", report.Diags)
			}
			assertMaterializationPathsUnchanged(t, plannedBefore)
			assertMaterializationPathsUnchanged(t, alternateBefore)
		})
	}
}

type materializationPathSnapshot struct {
	path    string
	exists  bool
	entries map[string]materializationPathEntry
}

type materializationPathEntry struct {
	mode os.FileMode
	data []byte
	link string
}

func plannedMachineDestinationPaths(target hcc.Target, home, codexHome string) []string {
	switch target {
	case hcc.TargetClaude:
		return []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json")}
	case hcc.TargetCodex:
		return []string{filepath.Join(home, ".agents"), filepath.Join(codexHome, ".codex")}
	default:
		return nil
	}
}

func seedPlannedMachineDestinations(t *testing.T, target hcc.Target, home, codexHome string) {
	t.Helper()
	for _, path := range plannedMachineDestinationPaths(target, home, codexHome) {
		if filepath.Ext(path) == ".json" {
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatalf("seed %s: %v", path, err)
			}
			continue
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "handwritten.txt"), []byte("keep\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
}

func snapshotMaterializationPaths(t *testing.T, paths []string) []materializationPathSnapshot {
	t.Helper()
	snapshots := make([]materializationPathSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot := materializationPathSnapshot{path: path, entries: make(map[string]materializationPathEntry)}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			snapshots = append(snapshots, snapshot)
			continue
		}
		if err != nil {
			t.Fatalf("snapshot %s: %v", path, err)
		}
		snapshot.exists = true
		if info.IsDir() {
			err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				return recordMaterializationPathEntry(snapshot.entries, current)
			})
			if err != nil {
				t.Fatalf("walk snapshot %s: %v", path, err)
			}
		} else {
			if err := recordMaterializationPathEntry(snapshot.entries, path); err != nil {
				t.Fatalf("read snapshot %s: %v", path, err)
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func recordMaterializationPathEntry(entries map[string]materializationPathEntry, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	snapshot := materializationPathEntry{mode: info.Mode()}
	if info.Mode().IsRegular() {
		snapshot.data, err = os.ReadFile(path)
	} else if info.Mode()&os.ModeSymlink != 0 {
		snapshot.link, err = os.Readlink(path)
	}
	if err != nil {
		return err
	}
	entries[path] = snapshot
	return nil
}

func assertMaterializationPathsUnchanged(t *testing.T, snapshots []materializationPathSnapshot) {
	t.Helper()
	for _, before := range snapshots {
		after := snapshotMaterializationPaths(t, []string{before.path})[0]
		if !reflect.DeepEqual(after, before) {
			t.Errorf("path changed after plan drift refusal: %s\n before: %#v\n after:  %#v", before.path, before, after)
		}
	}
}

func TestPlanExecuteRefusesPortableManifestDriftBeforeWrites(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved := []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "manifest-drift", Resource: ir.NewSkill("manifest-drift", "manifest drift", "body", nil, nil, ir.SourceRef{})},
		Source:    config.UserPersonal,
		Harnesses: []hcc.Target{hcc.TargetClaude},
	}}
	plan, err := BuildPlan(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	manifestPath := filepath.Join(home, ".agent-marketplace", "materialization-manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("version: 1\nentries:\n  - target: claude\n    path: skills/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := plan.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !hasDiagCode(report.Diags, "materialize.plan.drift") {
		t.Fatalf("missing plan drift diagnostic: %#v", report.Diags)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("manifest drift allowed output: %v", err)
	}
}

func TestBuildPlanRejectsUnknownPortableManifestTarget(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	manifestPath := filepath.Join(home, ".agent-marketplace", "materialization-manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("version: 1\nentries:\n  - target: unknown-harness\n    path: skills/unknown\n")
	if err := os.WriteFile(manifestPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(context.Background(), repoRoot, nil); err == nil || !strings.Contains(err.Error(), "unknown materialization manifest target") {
		t.Fatalf("BuildPlan error = %v, want unknown target rejection", err)
	}
	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("unknown-target manifest changed: got %q, want %q", got, before)
	}
}

func TestBuildPlanRejectsMalformedManagedNativeAggregateWithoutWriting(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(repoRoot, ".mcp.json")
	before := []byte(`{"mcpServers":{"stale":{"command":"server","_managed_by":"agent-marketplace"},`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPlan(context.Background(), repoRoot, nil); err == nil || !strings.Contains(err.Error(), "parse native aggregate") {
		t.Fatalf("BuildPlan error = %v, want native aggregate parse error", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("native aggregate changed: got %q, want %q", got, before)
	}
}

func TestBuildPlanStaleNativeAggregatePropagatesDestinationResolutionError(t *testing.T) {
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, ".mcp.json")
	before := []byte(`{"mcpServers":{"stale":{"command":"server","_managed_by":"agent-marketplace"}}}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	// Repo-scope native state revives Claude as a stale-only output. Machine
	// destination resolution must still fail closed instead of being skipped
	// merely because that scope was not seeded by resolved resources.
	t.Setenv("HOME", "")

	if _, err := BuildPlan(context.Background(), repoRoot, nil); err == nil || !strings.Contains(err.Error(), "native aggregate destinations") {
		t.Fatalf("BuildPlan error = %v, want destination-resolution error", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("native aggregate changed: got %q, want %q", got, before)
	}
}
