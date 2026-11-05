package harnesscompile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestMaterializeBookkeepingPreservesMixedGeneration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   WriteMode
		dryRun bool
		prune  bool
	}{
		{"overwrite", WriteModeOverwrite, false, false},
		{"sync", WriteModeSync, false, true},
		{"dry-overwrite", WriteModeOverwrite, true, false},
		{"dry-sync", WriteModeSync, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			original := map[string]string{"a.txt": "keep a\n", "b.txt": "old b\n", "c.txt": "keep c\n", "stale.txt": "stale\n"}
			before := make(map[string]os.FileInfo)
			for name, data := range original {
				path := filepath.Join(out, name)
				if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(1700000000, 0)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[name] = info
			}
			result := &Result{Artifacts: []Artifact{
				{Path: "d.txt", Bytes: []byte("new d\n"), Mode: 0o600},
				{Path: "c.txt", Bytes: []byte("keep c\n"), Mode: 0o700},
				{Path: "b.txt", Bytes: []byte("new b\n"), Mode: 0o600},
				{Path: "a.txt", Bytes: []byte("keep a\n"), Mode: 0o700},
			}}
			input := slices.Clone(result.Artifacts)
			for i := range input {
				input[i].Bytes = slices.Clone(input[i].Bytes)
			}
			opts := []WriteOption{WithWriteMode(tc.mode)}
			if tc.dryRun {
				opts = append(opts, WithDryRun())
			}
			report, err := Materialize(result, out, opts...)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(report.Written, []string{"b.txt", "d.txt"}) || !reflect.DeepEqual(report.Skipped, []string{"a.txt", "c.txt"}) {
				t.Fatalf("unexpected write/skip order: %+v", report)
			}
			if !reflect.DeepEqual(result.Artifacts, input) {
				t.Fatal("planning mutated caller artifacts")
			}
			var wantRemoved []string
			if tc.prune {
				wantRemoved = []string{"stale.txt"}
			}
			if !reflect.DeepEqual(report.Removed, wantRemoved) {
				t.Fatalf("unexpected removed paths: %+v", report)
			}
			for _, name := range []string{"a.txt", "b.txt", "c.txt", "stale.txt"} {
				data, err := os.ReadFile(filepath.Join(out, name))
				if name == "stale.txt" && tc.prune && !tc.dryRun {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("stale file not removed: %q, %v", data, err)
					}
					continue
				}
				want := original[name]
				if name == "b.txt" && !tc.dryRun {
					want = "new b\n"
				}
				if err != nil || string(data) != want {
					t.Fatalf("%s = %q, %v; want %q", name, data, err, want)
				}
				info, err := os.Stat(filepath.Join(out, name))
				if err != nil {
					t.Fatal(err)
				}
				if name != "b.txt" || tc.dryRun {
					if !os.SameFile(info, before[name]) || info.Mode() != before[name].Mode() || !info.ModTime().Equal(before[name].ModTime()) {
						t.Fatalf("retained file %s was replaced, chmodded or rewritten", name)
					}
				}
			}
			data, err := os.ReadFile(filepath.Join(out, "d.txt"))
			if tc.dryRun {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("dry run wrote new file: %q, %v", data, err)
				}
			} else if err != nil || !bytes.Equal(data, []byte("new d\n")) {
				t.Fatalf("new file = %q, %v", data, err)
			}
			if !tc.dryRun {
				for _, name := range []string{"b.txt", "d.txt"} {
					info, err := os.Stat(filepath.Join(out, name))
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatalf("written mode for %s = %v, %v", name, info, err)
					}
				}
			}
			assertNoStagingResidue(t, out)
		})
	}
}

func TestMaterializeBookkeepingRetainsNoOpRootRefusal(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	result := &Result{Artifacts: materializeBookkeepingArtifacts(4)}
	if _, err := Materialize(result, out); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(t.TempDir(), "retained")
	replaced := false
	materializeRootCaptureHook = func(path string) {
		replaced = true
		if err := os.Rename(path, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { materializeRootCaptureHook = nil })
	_, err := Materialize(result, out)
	if !replaced || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("unchanged input bypassed root admission: replaced=%v, err=%v", replaced, err)
	}
	for _, a := range result.Artifacts {
		data, err := os.ReadFile(filepath.Join(retained, a.Path))
		if err != nil || !bytes.Equal(data, a.Bytes) {
			t.Fatalf("retained file changed: %q, %v", data, err)
		}
	}
	assertNoStagingResidue(t, out)
}
