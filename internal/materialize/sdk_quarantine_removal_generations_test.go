package materialize

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/sdk-quarantined-payload-replacement-preserves-removal-generation
func TestSDKQuarantineRemovalPreservesReplacementGeneration(t *testing.T) {
	output := t.TempDir()
	retained := filepath.Join(t.TempDir(), "original-quarantined-payload")
	stale := filepath.Join(output, "stale.txt")
	if err := os.WriteFile(stale, []byte("inert selected stale payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(stale)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	replacement := ""
	var before os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-quarantine-remove" {
			return
		}
		changed = true
		replacement = path
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("foreign inert post-read quarantine replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}}))
	report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if raw, readErr := os.ReadFile(replacement); readErr != nil || string(raw) != "foreign inert post-read quarantine replacement" {
		t.Fatalf("SDK removed foreign post-read quarantine payload: hit=%t report=%+v err=%v raw=%q read=%v", changed, report, err, raw, readErr)
	}
	if !changed || !errors.Is(err, hcc.ErrStalePruneConflict) {
		t.Fatalf("SDK post-read quarantine replacement did not refuse: %+v %v", report, err)
	}
	if after, err := os.Stat(replacement); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign quarantine metadata changed: %v", err)
	}
	if raw, err := os.ReadFile(retained); err != nil || string(raw) != "inert selected stale payload" {
		t.Fatalf("original quarantined payload changed: %q %v", raw, err)
	}
	if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
		t.Fatalf("original quarantined generation metadata changed: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("refusal created unauthorized stale restoration: %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 {
		t.Fatalf("SDK refusal changed output membership/staging cleanup: %v %v", entries, err)
	}
	if err := os.Rename(retained, stale); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	if err != nil {
		t.Fatalf("healthy SDK quarantine removal failed: %+v %v", report, err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("healthy SDK did not actually prune original stale payload: %v", err)
	}
	if raw, err := os.ReadFile(replacement); err != nil || string(raw) != "foreign inert post-read quarantine replacement" {
		t.Fatalf("healthy SDK touched foreign quarantine replacement: %q %v", raw, err)
	}
}
