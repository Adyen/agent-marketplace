package harnesscompile

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestStaleDiscoveryDigestStaysUnderCapturedParent(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	sub := filepath.Join(outDir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("inert admitted stale payload\n")
	if err := os.WriteFile(filepath.Join(sub, "stale.md"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-sub")
	foreign := filepath.Join(t.TempDir(), "foreign-sub")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "stale.md"), []byte("inert foreign replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleRel := filepath.Join("sub", "stale.md")
	var retargeted, restored bool
	staleDigestHook = func(rel, phase string) {
		if rel != staleRel {
			return
		}
		switch phase {
		case "before":
			if err := os.Rename(sub, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, sub); err != nil {
				t.Fatal(err)
			}
			retargeted = true
		case "after":
			if err := os.Remove(sub); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(retained, sub); err != nil {
				t.Fatal(err)
			}
			restored = true
		}
	}
	t.Cleanup(func() { staleDigestHook = nil })
	stale, err := findStaleFiles(outDir, map[string]struct{}{})
	if !retargeted || !restored {
		t.Fatalf("digest seam not reached: retargeted=%t restored=%t", retargeted, restored)
	}
	if err != nil {
		t.Fatalf("temporary parent retarget redirected the stale digest: %v", err)
	}
	if len(stale) != 1 || stale[0].Path != "sub/stale.md" || stale[0].Digest != sha256.Sum256(original) {
		t.Fatalf("stale digest not taken from the captured parent: %+v", stale)
	}
}
