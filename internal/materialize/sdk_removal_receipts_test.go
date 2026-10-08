package materialize

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/sdk-removal-receipts-distinguish-refusal-absence-and-dry-run
func TestSDKRemovalReceiptsDistinguishActualDeletion(t *testing.T) {
	for _, mode := range []string{"conflict", "disappearance", "dry-run"} {
		t.Run(mode, func(t *testing.T) {
			output := t.TempDir()
			safe := filepath.Join(output, "a-safe.txt")
			unsafe := filepath.Join(output, "b-unsafe.txt")
			retained := filepath.Join(t.TempDir(), "original-unsafe")
			if err := os.WriteFile(safe, []byte("inert actual safe stale payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unsafe, []byte("inert selected unsafe stale payload"), 0o640); err != nil {
				t.Fatal(err)
			}
			original, err := os.Stat(unsafe)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			var foreign os.FileInfo
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
				if changed || mode == "dry-run" || phase != "sdk-stale-quarantine" || path != unsafe {
					return
				}
				changed = true
				if err := os.Rename(unsafe, retained); err != nil {
					t.Fatal(err)
				}
				if mode == "conflict" {
					if err := os.WriteFile(unsafe, []byte("foreign inert preserved unsafe payload"), 0o600); err != nil {
						t.Fatal(err)
					}
					foreign, err = os.Stat(unsafe)
					if err != nil {
						t.Fatal(err)
					}
				}
			}}))
			opts := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true)}
			if mode == "dry-run" {
				opts = append(opts, hcc.WithDryRun())
			}
			report, err := hcc.Materialize(&hcc.Result{}, output, opts...)
			if report == nil {
				t.Fatalf("SDK returned no removal report: %v", err)
			}
			expected := []string{"a-safe.txt"}
			if mode == "dry-run" {
				expected = append(expected, "b-unsafe.txt")
			}
			if !reflect.DeepEqual(report.Removed, expected) {
				t.Fatalf("SDK removal receipt claimed refused/absent deletion: reached=%t mode=%s removed=%v expected=%v err=%v", changed, mode, report.Removed, expected, err)
			}
			plan := make([]string, 0, len(report.StaleCandidates))
			for _, candidate := range report.StaleCandidates {
				plan = append(plan, candidate.Path)
			}
			if !reflect.DeepEqual(plan, []string{"a-safe.txt", "b-unsafe.txt"}) || changed != (mode != "dry-run") {
				t.Fatalf("SDK planning/consumer not exercised: changed=%t plan=%v %+v", changed, plan, report)
			}
			if mode == "conflict" {
				if !errors.Is(err, hcc.ErrStalePruneConflict) || !reflect.DeepEqual(report.PruneConflicts, []string{"b-unsafe.txt"}) {
					t.Fatalf("SDK lost actual prune conflict: %+v %v", report, err)
				}
				if raw, err := os.ReadFile(unsafe); err != nil || string(raw) != "foreign inert preserved unsafe payload" {
					t.Fatalf("SDK changed refused foreign payload: %q %v", raw, err)
				}
				if info, err := os.Stat(unsafe); err != nil || !os.SameFile(foreign, info) || foreign.Mode() != info.Mode() || !foreign.ModTime().Equal(info.ModTime()) {
					t.Fatalf("SDK changed foreign metadata: %v", err)
				}
			} else if err != nil {
				t.Fatalf("SDK valid absence/dry-run failed: %+v %v", report, err)
			}
			members := 0
			if mode == "conflict" {
				members = 1
			}
			if mode == "dry-run" {
				members = 2
			}
			if entries, err := os.ReadDir(output); err != nil || len(entries) != members {
				t.Fatalf("SDK stage cleanup/output members wrong: %v %v", entries, err)
			}
			if mode == "dry-run" {
				if raw, err := os.ReadFile(safe); err != nil || string(raw) != "inert actual safe stale payload" {
					t.Fatalf("SDK dry-run changed safe payload: %q %v", raw, err)
				}
				if info, err := os.Stat(unsafe); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("SDK dry-run changed unsafe generation: %v", err)
				}
			} else {
				if _, err := os.Lstat(safe); !os.IsNotExist(err) {
					t.Fatalf("SDK receipt safe deletion not real: %v", err)
				}
				if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
					t.Fatalf("SDK changed retained original: %v", err)
				}
				if mode == "conflict" {
					if err := os.Rename(unsafe, filepath.Join(t.TempDir(), "retained-foreign")); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Rename(retained, unsafe); err != nil {
					t.Fatal(err)
				}
			}
			report, err = hcc.Materialize(&hcc.Result{}, output, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
			healthy := []string{"b-unsafe.txt"}
			if mode == "dry-run" {
				healthy = []string{"a-safe.txt", "b-unsafe.txt"}
			}
			if err != nil || !reflect.DeepEqual(report.Removed, healthy) {
				t.Fatalf("SDK healthy real removal receipts wrong: %+v %v", report, err)
			}
			if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
				t.Fatalf("SDK healthy receipt not backed by actual pruning: %v %v", entries, err)
			}
		})
	}
}
