package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: materialization/sdk-merge-comparison-retains-original-state-and-absence
func TestSDKMergeComparisonRetainsOriginalInputGeneration(t *testing.T) {
	const original = "{\n // original comment\n \"mcpServers\": {\"manual\": {\"command\": \"inert original\"}},\n \"theme\": \"original\"\n}\n"
	const foreign = "{\n // concurrent comment\n \"mcpServers\": {\"manual\": {\"command\": \"inert concurrent\"}},\n \"theme\": \"concurrent\"\n}\n"
	for _, mutation := range []string{"replacement", "same-bytes", "in-place", "appeared", "disappeared"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", mutation, dryRun), func(t *testing.T) {
				out, retained := t.TempDir(), filepath.Join(t.TempDir(), "original.json")
				path := filepath.Join(out, "mcp.json")
				if mutation != "appeared" {
					if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
						t.Fatal(err)
					}
				}
				result := &hcc.Result{Artifacts: []hcc.Artifact{{Path: "mcp.json", Bytes: []byte(`{"mcpServers":{"managed":{"command":"inert desired"}}}`), Mode: 0o600, MergeWithExisting: true, ManagedByField: "_managed_by", ManagedByValue: "agent-marketplace"}}}
				changed := false
				var replacement os.FileInfo
				want := foreign
				if mutation == "same-bytes" {
					want = original
				}
				logger := slog.Default()
				t.Cleanup(func() { slog.SetDefault(logger) })
				slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, observed string) {
					if changed || phase != "sdk-merge-input" || observed != path {
						return
					}
					changed = true
					if mutation != "appeared" && mutation != "in-place" {
						if err := os.Rename(path, retained); err != nil {
							t.Fatal(err)
						}
					}
					if mutation == "disappeared" {
						return
					}
					if err := os.WriteFile(path, []byte(want), 0o640); err != nil {
						t.Fatal(err)
					}
					var err error
					replacement, err = os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
				}}))
				options := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeOverwrite)}
				if dryRun {
					options = append(options, hcc.WithDryRun())
				}
				report, err := hcc.Materialize(result, out, options...)
				if !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("merge input drift did not generation-refuse: changed=%v report=%+v err=%v", changed, report, err)
				}
				if mutation == "disappeared" {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("refusal recreated disappeared input: %v", err)
					}
				} else {
					if data, err := os.ReadFile(path); err != nil || string(data) != want {
						t.Fatalf("refusal consumed concurrent bytes: %q %v", data, err)
					}
					if after, err := os.Stat(path); err != nil || !os.SameFile(replacement, after) || replacement.Mode() != after.Mode() || !replacement.ModTime().Equal(after.ModTime()) {
						t.Fatalf("refusal changed concurrent metadata: %v", err)
					}
				}
				if mutation == "replacement" || mutation == "same-bytes" || mutation == "disappeared" {
					if data, err := os.ReadFile(retained); err != nil || string(data) != original {
						t.Fatalf("refusal changed retained original: %q %v", data, err)
					}
				}
				if stages, err := filepath.Glob(filepath.Join(out, ".tmp-*")); err != nil || len(stages) != 0 {
					t.Fatalf("merge refusal left staging: %v %v", stages, err)
				}
				control, err := hcc.Materialize(result, out, options...)
				if err != nil || len(control.Written) != 1 {
					t.Fatalf("healthy original/absence merge control failed: %+v %v", control, err)
				}
				if dryRun {
					if mutation == "disappeared" {
						if _, err := os.Stat(path); !os.IsNotExist(err) {
							t.Fatalf("dry-run created originally absent control: %v", err)
						}
					} else if data, err := os.ReadFile(path); err != nil || string(data) != want {
						t.Fatalf("dry-run changed healthy input: %q %v", data, err)
					}
				} else {
					data, err := os.ReadFile(path)
					if err != nil || !bytes.Contains(data, []byte("inert desired")) || mutation != "disappeared" && !bytes.Contains(data, []byte("manual")) {
						t.Fatalf("healthy retry did not actually merge: %q %v", data, err)
					}
					info, err := os.Stat(path)
					wantMode := os.FileMode(0o640)
					if mutation == "disappeared" {
						wantMode = 0o600
					}
					if err != nil || info.Mode().Perm() != wantMode {
						t.Fatalf("healthy shared/private mode changed: %v %v", info, err)
					}
				}
			})
		}
	}
}
