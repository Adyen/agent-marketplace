package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: materialization/durable-aggregate-reader-retains-canonical-root
func TestRunDurableAggregateReadRefusesRetargetedRoot(t *testing.T) {
	home, repo, foreign := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desired := nativeSourceBackedMCPResources(t, hcc.TargetGemini, config.RepoCommitted)
	installed, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native Gemini control failed: %+v %v", installed, err)
	}
	root := filepath.Join(repo, ".gemini")
	before, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil || !bytes.Contains(before, []byte("first")) {
		t.Fatalf("native Gemini payload absent: %q %v", before, err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "settings.json"), before, 0o640); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(repo, "retained-gemini")
	hit := false
	ownedAggregateLockAcquisitionHook = func(event ownership.LockEvent) {
		if hit || event.Kind != ownership.LockAcquired {
			return
		}
		hit = true
		if err := os.Rename(root, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, root); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { ownedAggregateLockAcquisitionHook = nil })
	report, err := Run(t.Context(), repo, desired)
	if !hit {
		t.Fatal("actual Gemini root-session fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("retargeted durable root was accepted: %+v %v", report, err)
	}
	for _, path := range []string{filepath.Join(foreign, "settings.json"), filepath.Join(retained, "settings.json")} {
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
			t.Fatalf("durable reader changed %s: %q %v", path, after, err)
		}
	}
}

func TestRunDurableAggregateReadRefusesClonedRootIdentity(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desired := nativeSourceBackedMCPResources(t, hcc.TargetGemini, config.RepoCommitted)
	installed, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native Gemini control failed: %+v %v", installed, err)
	}
	root := filepath.Join(repo, ".gemini")
	nativePath := filepath.Join(root, "settings.json")
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	if !bytes.Contains(nativeBefore, []byte("echo audit")) {
		t.Fatalf("source-backed native hook absent from aggregate: %q", nativeBefore)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	boundHook := false
	for _, id := range proof.OwnedIDs() {
		if id.Kind != ownership.KindHook {
			continue
		}
		entry, ok := proof.Entry(id)
		boundHook = boundHook || ok && entry.SourceDigest != "" && len(entry.SourceEvidence) == 0
	}
	if !boundHook {
		t.Fatalf("initial Gemini proof has no source-bound hook: %+v", proof.OwnedIDs())
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	rootInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(repo, "retained-gemini")
	var cloneInfo os.FileInfo
	hit := false
	ownedAggregateLockAcquisitionHook = func(event ownership.LockEvent) {
		if hit || event.Kind != ownership.LockAcquired {
			return
		}
		hit = true
		if err := os.Rename(root, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, rootInfo.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(nativePath, nativeBefore, nativeMode.Perm()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(nativePath, nativeMode.Perm()); err != nil {
			t.Fatal(err)
		}
		cloneInfo, err = os.Lstat(nativePath)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() { ownedAggregateLockAcquisitionHook = nil }()
	report, err := Run(t.Context(), repo, desired)
	if !hit {
		t.Fatal("actual Gemini root-session fixture did not run")
	}
	if err != nil || report == nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("cloned durable root was accepted instead of refusing its captured owner identity: %+v %v", report, err)
	}
	assertFileUnchanged(t, filepath.Join(retained, "settings.json"), nativeBefore, nativeMode, nativeMtime)
	cloneMode, cloneMtime := cloneInfo.Mode(), cloneInfo.ModTime()
	assertFileUnchanged(t, nativePath, nativeBefore, cloneMode, cloneMtime)
	if cloneAfter, err := os.Lstat(nativePath); err != nil || !os.SameFile(cloneInfo, cloneAfter) {
		t.Fatalf("cloned native aggregate identity changed after refusal: %v", err)
	}
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
}
