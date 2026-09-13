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
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, "codex-home"))
			resolved := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "drift", Resource: ir.NewSkill("drift", "drift", "body", nil, nil, ir.SourceRef{})},
				Source:    config.UserPersonal,
				Harnesses: []hcc.Target{test.target},
			}}
			plan, err := BuildPlan(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			test.mutate(t, home, alternate)
			report, err := plan.Execute(context.Background())
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !hasDiagCode(report.Diags, "materialize.plan.drift") {
				t.Fatalf("missing plan drift diagnostic: %#v", report.Diags)
			}
			if _, err := os.Stat(filepath.Join(alternate, ".claude")); !os.IsNotExist(err) {
				t.Fatalf("drifted HOME received output: %v", err)
			}
			if _, err := os.Stat(filepath.Join(alternate, ".codex")); !os.IsNotExist(err) {
				t.Fatalf("drifted CODEX_HOME received output: %v", err)
			}
		})
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
