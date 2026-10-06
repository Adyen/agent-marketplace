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

type sdkStagedObserver struct {
	slog.Handler
	mutate func(string, string)
}

func (h sdkStagedObserver) Handle(ctx context.Context, r slog.Record) error {
	phase, output, stage := "", "", ""
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "phase":
			phase = a.Value.String()
		case "out_dir":
			output = a.Value.String()
		case "staged":
			stage = a.Value.String()
		}
		return true
	})
	if phase == "sdk-staged" {
		h.mutate(output, stage)
	}
	return h.Handler.Handle(ctx, r)
}

// spec: materialization/sdk-publication-refuses-replaced-root-and-preserves-foreign-stage
func TestSDKPublicationRefusesReplacedRootAndPreservesForeignStage(t *testing.T) {
	for _, rel := range []string{"owned.txt", "nested/owned.txt"} {
		t.Run(rel, func(t *testing.T) { testSDKPublicationRootRetarget(t, rel) })
	}
}

func TestSDKPublicationRefusesReplacedStageDirectory(t *testing.T) {
	output, retained := t.TempDir(), filepath.Join(t.TempDir(), "retained-stage")
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: "owned.txt", Bytes: []byte("inert original payload"), Mode: 0o640}}}
	changed := false
	stagePath := ""
	var original os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStagedObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(root, stage string) {
		if changed {
			return
		}
		changed = true
		stagePath = stage
		if err := os.Rename(stage, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "owned.txt"), []byte("foreign replacement stage"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		original, err = os.Stat(filepath.Join(stage, "owned.txt"))
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if raw, readErr := os.ReadFile(filepath.Join(stagePath, "owned.txt")); readErr != nil || string(raw) != "foreign replacement stage" {
		t.Fatalf("SDK consumed replacement stage: hit=%t raw=%q err=%v", changed, raw, readErr)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("stage replacement did not refuse: %+v %v", report, err)
	}
	if after, err := os.Stat(filepath.Join(stagePath, "owned.txt")); err != nil || !os.SameFile(original, after) || original.Mode() != after.Mode() || !original.ModTime().Equal(after.ModTime()) {
		t.Fatalf("replacement stage changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(output, "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("replacement stage published: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "owned.txt")); err != nil || string(raw) != "inert original payload" {
		t.Fatalf("original retained stage changed: %q %v", raw, err)
	}
	report, err = hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("healthy publication failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(filepath.Join(output, "owned.txt")); err != nil || string(raw) != "inert original payload" {
		t.Fatalf("healthy output absent: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(stagePath, "owned.txt")); err != nil || string(raw) != "foreign replacement stage" {
		t.Fatalf("healthy cleanup deleted previous replacement: %q %v", raw, err)
	}
}

func testSDKPublicationRootRetarget(t *testing.T, rel string) {
	output := filepath.Join(t.TempDir(), "output")
	retained := filepath.Join(t.TempDir(), "retained-root")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	result := &hcc.Result{Artifacts: []artifact.Artifact{{Path: rel, Bytes: []byte("independently authored inert output"), Mode: 0o640}}}
	changed := false
	stageName := ""
	var foreignInfo os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStagedObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(root, stage string) {
		if changed {
			return
		}
		changed = true
		stageName = filepath.Base(stage)
		if err := os.Rename(root, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, stageName), 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, stageName, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("foreign inert staged bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		foreignInfo, err = os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(result, output, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	foreign := filepath.Join(output, stageName, rel)
	if raw, readErr := os.ReadFile(foreign); readErr != nil || string(raw) != "foreign inert staged bytes" {
		t.Fatalf("SDK publication consumed foreign stage: hit=%t raw=%q err=%v", changed, raw, readErr)
	}
	if !errors.Is(err, fsboundary.ErrGenerationConflict) || !changed {
		t.Fatalf("SDK root replacement did not refuse: hit=%t report=%+v err=%v", changed, report, err)
	}
	if after, err := os.Lstat(foreign); err != nil || !os.SameFile(foreignInfo, after) || foreignInfo.Mode() != after.Mode() || !foreignInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign stage generation changed: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != stageName {
		t.Fatalf("SDK changed replacement root members: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("SDK refusal did not clean original attempt stage: %v %v", entries, err)
	}
	report, err = hcc.Materialize(result, retained, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil || len(report.Written) != 1 {
		t.Fatalf("actual valid SDK publication failed: %+v %v", report, err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, rel)); err != nil || string(raw) != "independently authored inert output" {
		t.Fatalf("healthy actual output missing: %q %v", raw, err)
	}
	if info, err := os.Stat(filepath.Join(retained, rel)); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("healthy output mode changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(filepath.Dir(rel)) && entries[0].Name() != rel {
		t.Fatalf("healthy SDK left stage: %v %v", entries, err)
	}
	if raw, err := os.ReadFile(foreign); err != nil || string(raw) != "foreign inert staged bytes" {
		t.Fatalf("healthy publication changed replacement root: %q %v", raw, err)
	}
}
