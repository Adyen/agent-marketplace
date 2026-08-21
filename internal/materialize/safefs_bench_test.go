package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The confinement layer sits under every managed write, so each sync pays its
// cost once per output root plus once per artifact. These benchmarks isolate it
// from the end-to-end Run benchmarks, whose per-iteration b.TempDir() makes them
// too noisy to attribute a change to this layer.

// benchConfinedTree builds a harness-root-shaped tree and returns the root path.
func benchConfinedTree(b *testing.B, files int) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), ".factory")
	nested := filepath.Join(root, "skills", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		b.Fatal(err)
	}
	for i := range files {
		name := filepath.Join(nested, fmt.Sprintf("artifact-%04d.md", i))
		if err := os.WriteFile(name, []byte("# artifact\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

// BenchmarkConfinedFSOpenRoot measures opening the confinement root, which every
// materialization mode pays once per output key.
func BenchmarkConfinedFSOpenRoot(b *testing.B) {
	root := benchConfinedTree(b, 0)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		fs, err := openConfinedFS(root, true)
		if err != nil {
			b.Fatal(err)
		}
		if err := fs.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConfinedFSAtomicWrite measures the per-artifact cost of a checked
// write into a nested directory below the root.
func BenchmarkConfinedFSAtomicWrite(b *testing.B) {
	root := benchConfinedTree(b, 0)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	target := filepath.Join(root, "skills", "nested", "written.md")
	data := []byte("# written\n")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fs.AtomicWrite(target, data, 0o644); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConfinedFSAtomicWriteReconcilingUnchanged measures the shared-config
// write-back path when the destination already holds the desired bytes, which is
// what every sync after the first one does to mcp.json, hooks.json and
// plugin.json.
func BenchmarkConfinedFSAtomicWriteReconcilingUnchanged(b *testing.B) {
	root := benchConfinedTree(b, 0)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	target := filepath.Join(root, "mcp.json")
	data := []byte(`{"mcpServers":{"alpha":{"command":"alpha"}}}`)
	if _, err := fs.AtomicWriteReconciling(target, data, 0o600); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		wrote, err := fs.AtomicWriteReconciling(target, data, 0o600)
		if err != nil {
			b.Fatal(err)
		}
		if wrote {
			b.Fatal("unchanged destination was rewritten")
		}
	}
}

// BenchmarkConfinedFSAtomicWriteReconcilingChanged measures the same path when
// the bytes really differ, i.e. the cost of the mode-preserving write plus the
// comparison that decided it was needed.
func BenchmarkConfinedFSAtomicWriteReconcilingChanged(b *testing.B) {
	root := benchConfinedTree(b, 0)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	target := filepath.Join(root, "mcp.json")
	variants := [2][]byte{
		[]byte(`{"mcpServers":{"alpha":{"command":"alpha"}}}`),
		[]byte(`{"mcpServers":{"beta":{"command":"beta"}}}`),
	}
	if _, err := fs.AtomicWriteReconciling(target, variants[0], 0o600); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		wrote, err := fs.AtomicWriteReconciling(target, variants[i%2], 0o600)
		if err != nil {
			b.Fatal(err)
		}
		if !wrote && i > 0 {
			b.Fatal("changed destination was skipped")
		}
	}
}

// BenchmarkConfinedFSReadDir measures the directory listing every prune decision
// is derived from, including the sort that restores os.ReadDir's ordering.
func BenchmarkConfinedFSReadDir(b *testing.B) {
	const files = 256
	root := benchConfinedTree(b, files)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	dir := filepath.Join(root, "skills", "nested")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		entries, err := fs.ReadDir(dir)
		if err != nil {
			b.Fatal(err)
		}
		if len(entries) != files {
			b.Fatalf("got %d entries, want %d", len(entries), files)
		}
	}
}

// BenchmarkConfinedFSAtomicSymlink measures the per-skill cost of the symlink
// materialization mode.
func BenchmarkConfinedFSAtomicSymlink(b *testing.B) {
	root := benchConfinedTree(b, 0)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	target := filepath.Join(root, "skills", "nested", "link")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fs.AtomicSymlink("/nonexistent/cache/skill", target); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConfinedFSLstat measures the checked-resolution cost paid by drift
// detection, which inspects every existing output before deciding to write.
func BenchmarkConfinedFSLstat(b *testing.B) {
	const files = 64
	root := benchConfinedTree(b, files)
	fs, err := openConfinedFS(root, true)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	paths := make([]string, files)
	for i := range paths {
		paths[i] = filepath.Join(root, "skills", "nested", fmt.Sprintf("artifact-%04d.md", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if _, err := fs.Lstat(paths[i%files]); err != nil {
			b.Fatal(err)
		}
	}
}
