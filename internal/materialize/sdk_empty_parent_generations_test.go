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
)

// spec: materialization/sdk-empty-parent-replacement-preserves-directory-generation
func TestSDKEmptyParentReplacementPreservesDirectoryGeneration(t *testing.T) {
	output := t.TempDir()
	parent := filepath.Join(output, "nested")
	retained := filepath.Join(t.TempDir(), "original-parent")
	foreign := filepath.Join(t.TempDir(), "foreign-parent")
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "stale.txt"), []byte("inert selected stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-empty-parent" {
			return
		}
		changed = true
		if path != parent {
			t.Fatalf("unexpected SDK empty parent boundary: %s", path)
		}
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(foreign, parent); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	after, inspectErr := os.Stat(parent)
	if inspectErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("SDK deleted/changed foreign empty parent: hit=%t report=%+v err=%v inspect=%v", changed, report, err, inspectErr)
	}
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("SDK empty parent replacement reported success: %+v %v", report, err)
	}
	if retainedInfo, err := os.Stat(retained); err != nil || !os.SameFile(original, retainedInfo) || original.Mode() != retainedInfo.Mode() {
		t.Fatalf("original parent generation/mode changed: %v", err)
	}
	if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
		t.Fatalf("original retained empty parent changed: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "nested" {
		t.Fatalf("SDK refusal changed output membership/staging cleanup: %v %v", entries, err)
	}
	if err := os.Rename(parent, foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "stale.txt"), []byte("healthy stale retry"), 0o640); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil {
		t.Fatalf("healthy SDK cleanup failed: %+v %v", report, err)
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Fatalf("healthy SDK did not prune actual empty parent: %v", err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("healthy SDK changed foreign directory generation: %v", err)
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
		t.Fatalf("healthy SDK changed foreign retained directory: %v %v", entries, err)
	}
}

func TestSDKEmptyParentRefusalReportsOnlyReachedRemovals(t *testing.T) {
	output := t.TempDir()
	parent := filepath.Join(output, "nested")
	retained := filepath.Join(t.TempDir(), "original-parent")
	foreign := filepath.Join(t.TempDir(), "foreign-parent")
	if err := os.Mkdir(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "stale.txt"), []byte("inert selected stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(output, "zz-later.txt")
	if err := os.WriteFile(later, []byte("inert later stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-empty-parent" || path != parent {
			return
		}
		changed = true
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(foreign, parent); err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("empty-parent replacement did not refuse: hit=%t %+v %v", changed, report, err)
	}
	if report == nil || len(report.Removed) != 1 || report.Removed[0] != "nested/stale.txt" {
		t.Fatalf("refused prune reported unreached candidates as removed: %+v", report)
	}
	if data, readErr := os.ReadFile(later); readErr != nil || string(data) != "inert later stale payload" {
		t.Fatalf("unreached stale candidate changed: %q %v", data, readErr)
	}
}
