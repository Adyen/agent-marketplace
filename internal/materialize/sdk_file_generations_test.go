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

// spec: materialization/sdk-file-publication-preserves-replaced-destination-generation
func TestSDKFilePublicationPreservesReplacedDestinationGeneration(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "originally-absent", true: "originally-present"}[exists], func(t *testing.T) { testSDKDestinationGeneration(t, exists) })
	}
}

func testSDKDestinationGeneration(t *testing.T, exists bool) {
	output := t.TempDir()
	retained := filepath.Join(t.TempDir(), "original-file")
	payload := filepath.Join(output, "owned.txt")
	if exists {
		if err := os.WriteFile(payload, []byte("inert old managed bytes"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "owned.txt", Bytes: []byte("inert updated managed bytes"), Mode: 0o640}}}
	changed := false
	var replacement os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStagedObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(root, stage string) {
		if changed {
			return
		}
		changed = true
		if exists {
			if err := os.Rename(payload, retained); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(payload, []byte("foreign replacement bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		replacement, err = os.Stat(payload)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if raw, readErr := os.ReadFile(payload); readErr != nil || string(raw) != "foreign replacement bytes" {
		t.Fatalf("SDK overwrote replacement destination: hit=%t raw=%q err=%v", changed, raw, readErr)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("SDK stale file generation did not refuse: %+v %v", report, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(replacement, after) || replacement.Mode() != after.Mode() || !replacement.ModTime().Equal(after.ModTime()) {
		t.Fatalf("replacement destination metadata changed: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "owned.txt" {
		t.Fatalf("SDK file refusal left stage: %v %v", entries, err)
	}
	if exists {
		if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert old managed bytes" {
			t.Fatalf("SDK changed retained original: %q %v", raw, err)
		}
	}
	if err := os.Remove(payload); err != nil {
		t.Fatal(err)
	}
	if exists {
		if err := os.Rename(retained, payload); err != nil {
			t.Fatal(err)
		}
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("original generation did not allow actual update: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(payload); err != nil || string(raw) != "inert updated managed bytes" {
		t.Fatalf("healthy updated bytes absent: %q %v", raw, err)
	}
	if info, err := os.Stat(payload); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("healthy mode changed: %v", err)
	}
}
