package harnesscompile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// BenchmarkMaterializeLargeArtifacts measures the cost of materializing
// large artifact sets across dry-run, already-synced, and prune-stale cases.
func BenchmarkMaterializeLargeArtifacts(b *testing.B) {
	cases := []struct {
		name       string
		numFiles   int
		alreadySet bool
		dryRun     bool
	}{
		{"100_files_dry_run", 100, false, true},
		{"500_files_dry_run", 500, false, true},
		{"100_files_missing", 100, false, false},
		{"100_files_already_synced", 100, true, false},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			outDir := b.TempDir()
			artifacts := make([]artifact.Artifact, c.numFiles)
			for i := 0; i < c.numFiles; i++ {
				artifacts[i] = artifact.Artifact{
					Path:  fmt.Sprintf("file-%04d.txt", i),
					Bytes: []byte(fmt.Sprintf("content-%d", i)),
				}
			}

			if c.alreadySet {
				result := &Result{Artifacts: artifacts}
				if _, err := Materialize(result, outDir, WithWriteMode(WriteModeSync)); err != nil {
					b.Fatal(err)
				}
			}

			result := &Result{Artifacts: artifacts}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				testOutDir := b.TempDir()
				if c.alreadySet {
					// Copy the synced state.
					if err := copyDir(outDir, testOutDir); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()

				opts := []WriteOption{WithWriteMode(WriteModeSync), WithPruneStale(true)}
				if c.dryRun {
					opts = append(opts, WithDryRun())
				}
				if _, err := Materialize(result, testOutDir, opts...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMaterializePruneStale measures the prune-stale path for many
// stale files.
func BenchmarkMaterializePruneStale(b *testing.B) {
	cases := []int{50, 200}

	for _, n := range cases {
		b.Run(fmt.Sprintf("%d_stale", n), func(b *testing.B) {
			outDir := b.TempDir()
			currentFiles := make([]artifact.Artifact, n/2)
			// Create current files.
			for i := 0; i < n/2; i++ {
				currentFiles[i] = artifact.Artifact{
					Path:  fmt.Sprintf("current-%04d.txt", i),
					Bytes: []byte(fmt.Sprintf("content-%d", i)),
				}
			}
			result := &Result{Artifacts: currentFiles}
			if _, err := Materialize(result, outDir, WithWriteMode(WriteModeSync)); err != nil {
				b.Fatal(err)
			}
			// Create stale files manually.
			for i := 0; i < n; i++ {
				if err := os.WriteFile(filepath.Join(outDir, fmt.Sprintf("stale-%04d.txt", i)), []byte("stale"), 0o644); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				testOutDir := b.TempDir()
				if err := copyDir(outDir, testOutDir); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()

				if _, err := Materialize(result, testOutDir, WithWriteMode(WriteModeSync), WithPruneStale(true)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMaterializeMergeUpdateDiskIONoNetwork(b *testing.B) {
	out := b.TempDir()
	path := filepath.Join(out, "mcp.json")
	const existing = "{\n // handwritten benchmark control\n \"mcpServers\": {\"manual\": {\"command\": \"inert manual\"}},\n \"theme\": \"preserve\"\n}\n"
	result := mergeArtifact("mcp.json", `{"mcpServers":{"managed":{"command":"inert managed"}}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		if err := os.WriteFile(path, []byte(existing), 0o640); err != nil {
			b.Fatal(err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		report, err := Materialize(result, out, WithWriteMode(WriteModeOverwrite))
		if err != nil || len(report.Written) != 1 {
			b.Fatalf("real merge update failed: %+v %v", report, err)
		}
	}
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
