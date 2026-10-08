package materialize

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: owned-entry-reconciliation/orphan-removal-refuses-retargeted-authorizing-evidence
func TestOrphanRemovalRefusesRetargetedEvidenceAfterStaging(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetCodex} {
		for _, replacement := range []string{"alias", "same-json"} {
			t.Run(string(target)+"/"+replacement, func(t *testing.T) { testOrphanEvidenceAction(t, target, replacement) })
		}
	}
}

func testOrphanEvidenceAction(t *testing.T, target hcc.Target, replacement string) {
	t.Helper()
	repo, home := devinEnv(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	root := filepath.Join(repo, "."+string(target))
	rel := harness.DevinMcpConfigFile
	if target == hcc.TargetCodex {
		rel = "config.toml"
	}
	selection := devinMcpSelection("managed")
	selection[0].Harnesses = []hcc.Target{target}
	if _, err := Run(context.Background(), repo, selection); err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)
	previews, err := RepairPreviews(context.Background(), repo, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var file *RepairFile
	for _, preview := range previews {
		if preview.Target == target {
			file = repairFileFor(t, preview.Preview, rel)
		}
	}
	if file == nil {
		t.Fatal("actual selected native repair preview absent")
	}
	if len(file.Orphans) != 1 {
		t.Fatalf("actual evidence-backed orphan absent: %d", len(file.Orphans))
	}
	manifest := strings.TrimSuffix(file.Orphans[0].Candidate.Evidence().Source(), ".bak") + ".meta.json"
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "original-manifest")
	foreign := filepath.Join(t.TempDir(), "foreign-manifest")
	if err := os.WriteFile(foreign, original, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	nativeBefore, err := os.Lstat(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	file.store, err = ownership.Open(root, ownership.WithCrasher(repairPolicyCrasher(func(point ownership.CrashPoint) error {
		if point == ownership.CrashAfterStagedWrite && !changed {
			changed = true
			if err := os.Rename(manifest, retained); err != nil {
				return err
			}
			if replacement == "alias" {
				return os.Symlink(foreign, manifest)
			}
			return os.WriteFile(manifest, original, 0o600)
		}
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	want := fsboundary.ErrGenerationConflict
	if replacement == "alias" {
		want = fsboundary.ErrUnsafePath
	}
	if _, err := file.RemoveOrphan(0, true); !changed || !errors.Is(err, want) {
		t.Fatalf("retargeted evidence authorized deletion: changed=%t err=%v", changed, err)
	}
	if after, err := os.ReadFile(file.AbsFile()); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refusal changed native aggregate: %v", err)
	}
	if nativeAfter, err := os.Lstat(file.AbsFile()); err != nil || !os.SameFile(nativeBefore, nativeAfter) || nativeBefore.Mode() != nativeAfter.Mode() || !nativeBefore.ModTime().Equal(nativeAfter.ModTime()) {
		t.Fatalf("refusal changed native generation: %v", err)
	}
	if _, err := os.Lstat(file.store.StateFile()); !os.IsNotExist(err) {
		t.Fatalf("refusal published ownership state: %v", err)
	}
	if after, err := os.ReadFile(retained); err != nil || !bytes.Equal(original, after) {
		t.Fatalf("refusal changed retained original metadata: %v", err)
	}
	if after, err := os.ReadFile(foreign); err != nil || !bytes.Equal(original, after) {
		t.Fatalf("refusal changed foreign evidence: %v", err)
	}
	if replacement == "alias" {
		if link, err := os.Readlink(manifest); err != nil || link != foreign {
			t.Fatalf("refusal removed evidence replacement: %q %v", link, err)
		}
	} else if raw, err := os.ReadFile(manifest); err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("refusal changed replacement evidence: %v", err)
	}
	if journal, err := file.store.ReadJournal(); err != nil || journal != nil {
		t.Fatalf("refused removal retained attempt journal: %v", err)
	}
	if stages, err := filepath.Glob(file.AbsFile() + ".am-staged-*"); err != nil || len(stages) != 0 {
		t.Fatalf("refused removal retained staging: %v %v", stages, err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, manifest); err != nil {
		t.Fatal(err)
	}
	if result, err := file.RemoveOrphan(0, true); err != nil || result.Generation == 0 || result.BackupPath == "" {
		t.Fatalf("restored evidence did not permit real backed-up deletion: %+v %v", result, err)
	}
	if after, err := os.ReadFile(file.AbsFile()); err != nil || strings.Contains(string(after), "managed-binary") {
		t.Fatalf("actual orphan was not removed: %s %v", after, err)
	}
}
