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

// spec: materialization/sdk-stale-discovery-refuses-directory-replacement-before-importing-members
func TestSDKStaleDiscoveryRefusesDirectoryReplacementBeforeImportingMembers(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "dry-run"}[dry], func(t *testing.T) { testSDKStaleDirectoryReplacement(t, dry) })
	}
}
func testSDKStaleDirectoryReplacement(t *testing.T, dry bool) {
	output := t.TempDir()
	parent := filepath.Join(output, "nested")
	retained := filepath.Join(t.TempDir(), "selected-dir")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "selected.txt"), []byte("selected inert stale bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	changed := false
	var foreign os.FileInfo
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
		if changed || phase != "sdk-stale-directory" || path != parent {
			return
		}
		changed = true
		if err := os.Rename(parent, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, "foreign.txt"), []byte("foreign inert directory member"), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		foreign, err = os.Stat(filepath.Join(parent, "foreign.txt"))
		if err != nil {
			t.Fatal(err)
		}
	}}))
	opts := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true)}
	if dry {
		opts = append(opts, hcc.WithDryRun())
	}
	report, err := hcc.Materialize(&hcc.Result{}, output, opts...)
	if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("SDK imported replacement directory members: hit=%t report=%+v err=%v", changed, report, err)
	}
	foreignPath := filepath.Join(parent, "foreign.txt")
	if raw, err := os.ReadFile(foreignPath); err != nil || string(raw) != "foreign inert directory member" {
		t.Fatalf("foreign directory bytes changed: %q %v", raw, err)
	}
	if after, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreign, after) || foreign.Mode() != after.Mode() || !foreign.ModTime().Equal(after.ModTime()) {
		t.Fatalf("foreign directory metadata changed: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(retained, "selected.txt")); err != nil || string(raw) != "selected inert stale bytes" {
		t.Fatalf("selected directory changed: %q %v", raw, err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 1 || entries[0].Name() != "nested" {
		t.Fatalf("SDK discovery refusal left stage: %v %v", entries, err)
	}
	if err := os.Remove(foreignPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, parent); err != nil {
		t.Fatal(err)
	}
	report, err = hcc.Materialize(&hcc.Result{}, output, opts...)
	if err != nil || len(report.StaleCandidates) != 1 || report.StaleCandidates[0].Path != "nested/selected.txt" {
		t.Fatalf("restored directory discovery failed: %+v %v", report, err)
	}
	if dry {
		if raw, err := os.ReadFile(filepath.Join(parent, "selected.txt")); err != nil || string(raw) != "selected inert stale bytes" {
			t.Fatalf("healthy dry-run mutated payload: %q %v", raw, err)
		}
	} else {
		if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
			t.Fatalf("actual healthy prune did not remove selected member/emptyparent/stage: %v %v", entries, err)
		}
	}
}
