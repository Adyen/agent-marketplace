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

// spec: materialization/sdk-quarantine-restoration-preserves-observed-generation
func TestSDKQuarantineRestorePreservesObservedGeneration(t *testing.T) {
	for _, phase := range []string{"sdk-stale-quarantine-restore", "sdk-stale-quarantine-restore-linked"} {
		t.Run(phase, func(t *testing.T) {
			output := t.TempDir()
			stale := filepath.Join(output, "stale.txt")
			original := filepath.Join(t.TempDir(), "original-stale")
			retained := filepath.Join(t.TempDir(), "observed-quarantined-replacement")
			if err := os.WriteFile(stale, []byte("inert original stale payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			selectedInfo, err := os.Stat(stale)
			if err != nil {
				t.Fatal(err)
			}
			compared, replaced := false, false
			quarantine := ""
			var observed, foreign os.FileInfo
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(currentPhase, path string) {
				if !compared && currentPhase == "sdk-stale-quarantine" {
					compared = true
					if err := os.Rename(stale, original); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(stale, []byte("observed first inert stale replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
					observed, err = os.Stat(stale)
					if err != nil {
						t.Fatal(err)
					}
				}
				if !replaced && currentPhase == phase {
					replaced = true
					if !filepath.IsAbs(path) {
						path = filepath.Join(output, path)
					}
					quarantine = path
					if err := os.Rename(path, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("foreign second inert quarantine replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
					foreign, err = os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
				}
			}}))
			report, err := hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
			if raw, readErr := os.ReadFile(quarantine); readErr != nil || string(raw) != "foreign second inert quarantine replacement" {
				t.Fatalf("SDK moved/deleted unauthorized restore generation: reached=%t/%t report=%+v err=%v raw=%q read=%v", compared, replaced, report, err, raw, readErr)
			}
			if !compared || !replaced || !errors.Is(err, hcc.ErrStalePruneConflict) {
				t.Fatalf("SDK restore replacement did not retain original conflict: %+v %v", report, err)
			}
			if info, err := os.Stat(quarantine); err != nil || !os.SameFile(foreign, info) || foreign.Mode() != info.Mode() || !foreign.ModTime().Equal(info.ModTime()) {
				t.Fatalf("foreign restore generation metadata changed: %v", err)
			}
			if raw, err := os.ReadFile(original); err != nil || string(raw) != "inert original stale payload" {
				t.Fatalf("selected original changed: %q %v", raw, err)
			}
			if info, err := os.Stat(original); err != nil || !os.SameFile(selectedInfo, info) || selectedInfo.Mode() != info.Mode() || !selectedInfo.ModTime().Equal(info.ModTime()) {
				t.Fatalf("selected original metadata changed: %v", err)
			}
			if raw, err := os.ReadFile(retained); err != nil || string(raw) != "observed first inert stale replacement" {
				t.Fatalf("observed quarantined generation changed: %q %v", raw, err)
			}
			if info, err := os.Stat(retained); err != nil || !os.SameFile(observed, info) || observed.Mode() != info.Mode() || !observed.ModTime().Equal(info.ModTime()) {
				t.Fatalf("observed quarantined metadata changed: %v", err)
			}
			members := 1
			if phase == "sdk-stale-quarantine-restore-linked" {
				members++
				if raw, err := os.ReadFile(stale); err != nil || string(raw) != "observed first inert stale replacement" {
					t.Fatalf("legitimate observed restoration changed: %q %v", raw, err)
				}
				if info, err := os.Stat(stale); err != nil || !os.SameFile(observed, info) {
					t.Fatalf("legitimate restored inode changed: %v", err)
				}
			} else if _, err := os.Lstat(stale); !os.IsNotExist(err) {
				t.Fatalf("unauthorized replacement restored at stale name: %v", err)
			}
			if entries, err := os.ReadDir(output); err != nil || len(entries) != members {
				t.Fatalf("restore refusal changed output membership/staging cleanup: %v %v", entries, err)
			}
			if phase == "sdk-stale-quarantine-restore-linked" {
				if err := os.Remove(stale); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(original, stale); err != nil {
				t.Fatal(err)
			}
			report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
			if err != nil {
				t.Fatalf("healthy actual SDK prune failed: %+v %v", report, err)
			}
			if _, err := os.Lstat(stale); !os.IsNotExist(err) {
				t.Fatalf("healthy SDK did not prune genuine original: %v", err)
			}
			if raw, err := os.ReadFile(quarantine); err != nil || string(raw) != "foreign second inert quarantine replacement" {
				t.Fatalf("healthy retry changed foreign retained quarantine: %q %v", raw, err)
			}
		})
	}
}
