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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/durable-aggregate-reader-retains-canonical-root
func TestRunDurableAggregateReadRefusesRetargetedRoot(t *testing.T) {
	home, repo, foreign := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desired := nativeMCPResources(hcc.TargetGemini, config.RepoCommitted)
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

// spec: materialization/gemini-copied-replacement-root-refuses-after-lock-acquisition
func TestRunDurableAggregateReadRefusesOrdinaryRootReplacement(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desired := nativeMCPResources(hcc.TargetGemini, config.RepoCommitted)
	installed, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native Gemini control failed: %+v %v", installed, err)
	}
	root := filepath.Join(repo, ".gemini")
	before, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil || !bytes.Contains(before, []byte("first")) {
		t.Fatalf("native Gemini payload absent: %q %v", before, err)
	}
	// Build an ordinary foreign directory with a copied identical aggregate.
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "settings.json"), before, 0o600); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Lstat(filepath.Join(root, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	foreignInfo, err := os.Lstat(filepath.Join(foreign, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(repo, "retained-gemini")
	hit := false
	var replaceErr error
	ownedAggregateLockAcquisitionHook = func(event ownership.LockEvent) {
		if hit || event.Kind != ownership.LockAcquired {
			return
		}
		hit = true
		if replaceErr = os.Rename(root, retained); replaceErr != nil {
			return
		}
		// Ordinary directory replacement, not a symlink.
		replaceErr = os.Rename(foreign, root)
	}
	t.Cleanup(func() { ownedAggregateLockAcquisitionHook = nil })
	report, err := Run(t.Context(), repo, desired)
	if replaceErr != nil {
		t.Fatal(replaceErr)
	}
	if !hit {
		t.Fatal("actual Gemini root-session fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("ordinary replacement root was accepted: %+v %v", report, err)
	}
	// Both directory generations are preserved with their payload bytes.
	for _, item := range []struct {
		root string
		info os.FileInfo
	}{
		{root, foreignInfo},
		{retained, originalInfo},
	} {
		path := filepath.Join(item.root, "settings.json")
		if after, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("durable reader changed %s: %q %v", path, after, readErr)
		}
		if after, err := os.Lstat(path); err != nil || !os.SameFile(item.info, after) || item.info.Mode() != after.Mode() || !item.info.ModTime().Equal(after.ModTime()) {
			t.Fatalf("durable reader changed aggregate metadata: %s %v", path, err)
		}
		if entries, err := os.ReadDir(item.root); err != nil || len(entries) != 1 || entries[0].Name() != "settings.json" {
			t.Fatalf("durable reader changed root membership: %v %v", entries, err)
		}
	}
	// Positive control: restoring the original root allows actual materialization.
	if err := os.Rename(root, foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, root); err != nil {
		t.Fatal(err)
	}
	desired[0].Resource = ir.NewMcpServer("first", "echo", []string{"updated-root-control"}, nil, nil, ir.SourceRef{})
	restored, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(restored.Diags) {
		t.Fatalf("restored original root sync failed: %+v %v", restored, err)
	}
	if after, err := os.ReadFile(filepath.Join(root, "settings.json")); err != nil || !bytes.Contains(after, []byte("updated-root-control")) {
		t.Fatalf("restored original root did not publish the changed desired aggregate: %q %v", after, err)
	}
}

// spec: materialization/gemini-copied-replacement-root-refuses-after-lock-acquisition
func TestRunDurableAggregateReadFreshRootInstallation(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desired := nativeMCPResources(hcc.TargetGemini, config.RepoCommitted)
	// Remove the .gemini root before the second Run to prove fresh-root
	// installation still works after the admission identity check.
	first, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(first.Diags) {
		t.Fatalf("first install failed: %+v %v", first, err)
	}
	root := filepath.Join(repo, ".gemini")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	second, err := Run(t.Context(), repo, desired)
	if err != nil || diag.HasErrors(second.Diags) {
		t.Fatalf("fresh-root re-install failed: %+v %v", second, err)
	}
	after, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil || !bytes.Contains(after, []byte("first")) {
		t.Fatalf("fresh-root payload absent: %q %v", after, err)
	}
}
