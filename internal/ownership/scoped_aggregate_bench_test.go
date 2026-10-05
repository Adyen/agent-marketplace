package ownership

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkScopedAggregateReadDisk(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "aggregate.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		b.Fatal(err)
	}
	store, err := Open(root, WithStateRoot(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	for _, scoped := range []bool{false, true} {
		name := "legacy"
		if scoped {
			name = "captured"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var data []byte
				var err error
				if scoped {
					data, err = store.ReadAggregate("aggregate.json")
				} else {
					data, err = ReadAggregateFile(path)
				}
				if err != nil || len(data) != 4096 {
					b.Fatalf("aggregate read control failed: %d %v", len(data), err)
				}
			}
		})
	}
}

// BenchmarkStoreOpenWithRootAdmission measures the cost of capturing the
// admitted root identity at Open time. The root exists and contains a small
// aggregate. Disk work includes canonicalization, descriptor-relative root
// capture, and descriptor cleanup. Timing scope: Open only, not payload reads,
// filesystem writes, or network access.
func BenchmarkStoreOpenWithRootAdmission(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aggregate.json"), bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		b.Fatal(err)
	}
	stateRoot := b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		store, err := Open(root, WithStateRoot(stateRoot))
		if err != nil {
			b.Fatal(err)
		}
		_ = store
	}
}

// BenchmarkStoreOpenAbsentRoot measures the cost of Open when the root is
// genuinely absent. It includes canonicalization and the failed root-capture
// attempt, without filesystem writes or network access.
func BenchmarkStoreOpenAbsentRoot(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	parent := b.TempDir()
	root := filepath.Join(parent, "absent-root")
	stateRoot := b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		store, err := Open(root, WithStateRoot(stateRoot))
		if err != nil {
			b.Fatal(err)
		}
		_ = store
	}
}

// BenchmarkScopedAggregateReadWithAdmission measures the full Open + ReadAggregate
// path with the admitted root identity check active. Disk work includes
// canonicalization, descriptor-relative root capture and cleanup, destination
// capture, and a 4 KiB read. Timing scope: one Open and one ReadAggregate per
// iteration, without filesystem writes or network access.
func BenchmarkScopedAggregateReadWithAdmission(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aggregate.json"), bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		b.Fatal(err)
	}
	stateRoot := b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		store, err := Open(root, WithStateRoot(stateRoot))
		if err != nil {
			b.Fatal(err)
		}
		data, err := store.ReadAggregate("aggregate.json")
		if err != nil || len(data) != 4096 {
			b.Fatalf("aggregate read failed: %d %v", len(data), err)
		}
	}
}
