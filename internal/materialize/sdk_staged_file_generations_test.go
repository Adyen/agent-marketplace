package materialize

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// spec: materialization/sdk-staged-file-replacement-preserves-payload-and-refuses-publication
func TestSDKStagedFileReplacementPreservesPayloadAndRefusesPublication(t *testing.T) {
	output := t.TempDir()
	retained := filepath.Join(t.TempDir(), "original-staged-file")
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "owned.txt", Bytes: []byte("inert source payload"), Mode: 0o640}}}
	changed := false
	stagePath := ""
	var replacement os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStagedObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(root, stage string) {
		if changed {
			return
		}
		changed = true
		stagePath = stage
		leaf := filepath.Join(stage, "owned.txt")
		if err := os.Rename(leaf, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(leaf, []byte("foreign staged replacement bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		replacement, err = os.Stat(leaf)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	leaf := filepath.Join(stagePath, "owned.txt")
	if raw, readErr := os.ReadFile(leaf); readErr != nil || string(raw) != "foreign staged replacement bytes" {
		t.Fatalf("SDK consumed/deleted foreign staged replacement: hit=%t raw=%q err=%v", changed, raw, readErr)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("staged file did not refuse: %+v %v", report, err)
	}
	if after, err := os.Stat(leaf); err != nil || !os.SameFile(replacement, after) || replacement.Mode() != after.Mode() || !replacement.ModTime().Equal(after.ModTime()) {
		t.Fatalf("replacement staged file metadata changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(output, "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("staged replacement published: %v", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert source payload" {
		t.Fatalf("retained original stage changed: %q %v", raw, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(stagePath) {
		t.Fatalf("refusal changed output membership: %v %v", entries, err)
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("healthy actual SDK publication failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(filepath.Join(output, "owned.txt")); err != nil || string(raw) != "inert source payload" {
		t.Fatalf("healthy actual bytes absent: %q %v", raw, err)
	}
	if info, err := os.Stat(filepath.Join(output, "owned.txt")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("healthy source mode changed: %v", err)
	}
	if raw, err := os.ReadFile(leaf); err != nil || string(raw) != "foreign staged replacement bytes" {
		t.Fatalf("healthy cleanup deleted previous replacement: %q %v", raw, err)
	}
}
