package materialize

import (
	"context"
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

type sdkRootAdmissionObserver struct {
	slog.Handler
	phase  string
	mutate func(string)
}

func (h sdkRootAdmissionObserver) Handle(ctx context.Context, r slog.Record) error {
	phase, path := "", ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "phase" {
			phase = a.Value.String()
		}
		if a.Key == "out_dir" {
			path = a.Value.String()
		}
		return true
	})
	wanted := h.phase
	if wanted == "" {
		wanted = "sdk-root-admitted"
	}
	if phase == wanted {
		h.mutate(path)
	}
	return h.Handler.Handle(ctx, r)
}

func TestSDKStageWritesStayUnderAdmittedDirectory(t *testing.T) {
	output, foreign := t.TempDir(), t.TempDir()
	retained := filepath.Join(t.TempDir(), "retained-stage")
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "nested/owned.txt", Bytes: []byte("inert original stage payload"), Mode: 0o640}}}
	changed := false
	stagePath := ""
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkRootAdmissionObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), phase: "sdk-stage-admitted", mutate: func(stage string) {
		if changed {
			return
		}
		changed = true
		stagePath = stage
		if err := os.Rename(stage, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(foreign, stage); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("staged writer did not refuse changed directory: %+v %v", report, err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
		t.Fatalf("SDK staged writer escaped captured directory: %v %v", entries, err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "nested", "owned.txt")); err != nil || string(raw) != "inert original stage payload" {
		t.Fatalf("writer did not remain on admitted stage: %q %v", raw, err)
	}
	if _, err := os.Lstat(filepath.Join(output, "nested")); !os.IsNotExist(err) {
		t.Fatalf("refused staged output published: %v", err)
	}
	if target, err := os.Readlink(stagePath); err != nil || target != foreign {
		t.Fatalf("attempt cleanup changed replacement alias: %q %v", target, err)
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("healthy actual staged publication failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(filepath.Join(output, "nested", "owned.txt")); err != nil || string(raw) != "inert original stage payload" {
		t.Fatalf("healthy payload absent: %q %v", raw, err)
	}
}

// spec: materialization/sdk-stage-creation-refuses-replaced-output-root
func TestSDKStageCreationRefusesReplacedOutputRoot(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output")
	retained := filepath.Join(t.TempDir(), "retained-root")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "nested/owned.txt", Bytes: []byte("inert staged payload"), Mode: 0o640}}}
	changed := false
	var foreignInfo os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkRootAdmissionObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(root string) {
		if changed {
			return
		}
		changed = true
		if err := os.Rename(root, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(root, "foreign.txt")
		if err := os.WriteFile(foreign, []byte("foreign inert bytes"), 0o640); err != nil {
			t.Fatal(err)
		}
		var err error
		foreignInfo, err = os.Stat(foreign)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if entries, readErr := os.ReadDir(output); readErr != nil || len(entries) != 1 || entries[0].Name() != "foreign.txt" {
		t.Fatalf("SDK stage creation changed foreign namespace: hit=%t entries=%v err=%v", changed, entries, readErr)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("SDK stage namespace did not refuse: %+v %v", report, err)
	}
	foreign := filepath.Join(output, "foreign.txt")
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "foreign inert bytes" {
		t.Fatalf("foreign bytes changed: %q %v", raw, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(foreignInfo, after) || foreignInfo.Mode() != after.Mode() || !foreignInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign generation changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("refusal left original attempt members: %v %v", entries, err)
	}
	report, err = hcc.Materialize(result, retained, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("actual healthy SDK stage/publication failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "nested", "owned.txt")); err != nil || string(raw) != "inert staged payload" {
		t.Fatalf("healthy staged bytes absent: %q %v", raw, err)
	}
	if info, err := os.Stat(filepath.Join(retained, "nested", "owned.txt")); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("healthy stage mode changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 1 || entries[0].Name() != "nested" {
		t.Fatalf("healthy SDK left staging: %v %v", entries, err)
	}
}
