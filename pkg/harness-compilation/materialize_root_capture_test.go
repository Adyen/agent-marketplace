package harnesscompile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestMaterializeRefusesReplacedRootWithoutStagingResidue(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	first := &Result{Artifacts: []artifact.Artifact{{Path: "a.md", Bytes: []byte("inert first")}}}
	if _, err := Materialize(first, outDir); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained-out")
	replaced := false
	materializeRootCaptureHook = func(dir string) {
		if replaced || dir != outDir {
			return
		}
		replaced = true
		if err := os.Rename(outDir, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { materializeRootCaptureHook = nil })
	second := &Result{Artifacts: []artifact.Artifact{{Path: "a.md", Bytes: []byte("inert second")}}}
	_, err := Materialize(second, outDir, WithWriteMode(WriteModeOverwrite))
	if !replaced {
		t.Fatalf("root capture seam not reached: %v", err)
	}
	if !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("replaced output root not refused: %v", err)
	}
	entries, readErr := os.ReadDir(outDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("refusal left staging residue in the replacement root: %s", entry.Name())
		}
	}
	if data, err := os.ReadFile(filepath.Join(retained, "a.md")); err != nil || string(data) != "inert first" {
		t.Fatalf("retained admitted output changed: %q %v", data, err)
	}
}
