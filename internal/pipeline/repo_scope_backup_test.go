package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// scopedSkill returns one resolved artifact whose Source puts it at the given
// scope. config.RepoCommitted is repo scope and config.UserPersonal is machine
// scope (config.Source.IsMachineScope).
func scopedSkill(target hcc.Target, source config.Source) []resolver.ResolvedArtifact {
	return []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Kind: ir.KindSkill, Name: "managed-skill", Resource: ir.NewSkill("managed-skill", "Test", "Body", nil, nil, ir.SourceRef{})},
		Source:    source,
		Harnesses: []hcc.Target{target},
	}}
}

// TestCollectManagedPathsIsMachineScopeOnlyByDecision pins the scope filter that
// bead agent-marketplace-5exj.30 decided to keep.
//
// Repo-scope aggregates are real files this tool merges into, and after bead
// .27 made harness.TargetInfo.AggregateFiles.Project truthful, collecting them
// here would be a two-line edit. It is deliberately not made: internal/backup's
// archive namespace has no root dimension (see
// TestBackupArchiveNamespaceHasNoRootDimension below and the internal/backup
// package doc), and internal/ownership — which IS root-keyed, mandatory, and
// already used at both scopes — is where repo-scope recovery lives.
//
// The test is non-vacuous in both directions: the machine-scope row proves the
// collector is capable of producing paths for exactly the same target and
// exactly the same resource, so an empty repo-scope result is the filter at
// work rather than a broken fixture.
func TestCollectManagedPathsIsMachineScopeOnlyByDecision(t *testing.T) {
	tests := []struct {
		name        string
		source      config.Source
		wantCollect bool
	}{
		{name: "organization is machine scope", source: config.Organization, wantCollect: true},
		{name: "user personal is machine scope", source: config.UserPersonal, wantCollect: true},
		{name: "repo committed is repo scope", source: config.RepoCommitted, wantCollect: false},
		{name: "repo personal is repo scope", source: config.RepoPersonal, wantCollect: false},
		{name: "unset defaults to repo scope", source: config.SourceUnset, wantCollect: false},
	}

	for _, target := range declaredTargets(t) {
		for _, tt := range tests {
			t.Run(string(target)+"/"+tt.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())

				managed := CollectManagedPaths(scopedSkill(target, tt.source))
				mp, ok := managed[target]
				if tt.wantCollect {
					if !ok || len(mp.RelPaths) == 0 {
						t.Fatalf("machine-scope source %s collected nothing for %s; the repo-scope rows below would be vacuous", tt.source, target)
					}
					return
				}
				if ok {
					t.Errorf("repo-scope source %s collected %v for %s.\n"+
						"internal/backup is machine scope BY DECISION (bead agent-marketplace-5exj.30): its archives are keyed by target alone, so a repo-scope archive would collide with every other repository's and with the machine root that `backup restore --target %s` overlays into.\n"+
						"Repo-scope aggregates are recovered through internal/ownership instead. Read the internal/backup package doc before changing this.",
						tt.source, mp.RelPaths, target, target)
				}
			})
		}
	}
}

// TestBackupArchiveNamespaceHasNoRootDimension is the measured evidence behind
// the decision recorded in the internal/backup package doc: extending that
// package to repo scope is a store redesign, not a collector change.
//
// backup.Create writes to <backupsDir>/<target>/<timestamp>.tar.gz and
// backup.Restore takes only a target, so NOTHING in the layout, in the archive
// name, or in the restore signature records which root an archive came from.
// This test archives the same target from two different roots holding different
// content and shows the second archive is what Restore hands back for the first
// root — the exact cross-contamination a naive repo-scope collector would ship,
// where the two roots are two checkouts of two repositories (or a repository and
// the machine root).
//
// Non-vacuity: the two roots are written with different content by design, and
// both archives are asserted to exist, so the mismatch below cannot come from an
// empty or single-archive namespace.
func TestBackupArchiveNamespaceHasNoRootDimension(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const target = hcc.TargetCursor
	const rel = "mcp.json"

	rootA := t.TempDir()
	rootB := t.TempDir()
	contentA := "{\n  // repo A handwritten\n  \"mcpServers\": {}\n}\n"
	contentB := "{\n  // repo B handwritten\n  \"mcpServers\": {}\n}\n"
	writeAggregate(t, rootA, rel, contentA)
	writeAggregate(t, rootB, rel, contentB)

	for _, root := range []string{rootA, rootB} {
		if err := backup.Create(map[hcc.Target]backup.ManagedPaths{
			target: {Root: root, RelPaths: []string{rel}},
		}); err != nil {
			t.Fatalf("backup.Create for %s: %v", root, err)
		}
	}

	archives, err := backup.ListBackups()
	if err != nil {
		t.Fatalf("backup.ListBackups: %v", err)
	}
	if got := len(archives[target]); got != 2 {
		t.Fatalf("expected two archives in the %s namespace, got %d (%v); the collision below would be vacuous", target, got, archives[target])
	}
	if len(archives) != 1 {
		t.Fatalf("expected exactly one target directory, got %v; the namespace has grown a dimension this test does not model", archives)
	}

	// Restore "repo A" from its own backup. There is no way to ask for it.
	restoreDir := t.TempDir()
	if _, err := backup.Restore(target, restoreDir); err != nil {
		t.Fatalf("backup.Restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(restoreDir, rel))
	if err != nil {
		t.Fatalf("read restored %s: %v", rel, err)
	}
	if string(got) != contentB {
		t.Fatalf("restore returned %q; this test asserts the CURRENT layout, in which the newest archive for a target wins regardless of root", got)
	}
	t.Logf("restoring root %s returned the bytes archived from root %s: the archive namespace is keyed by target alone, "+
		"which is why repo-scope coverage here needs a root-keyed layout (internal/backup package doc, point 1)", rootA, rootB)
}

// writeAggregate writes content at root/rel, creating parents.
func writeAggregate(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
