package dependencytxn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkReadLayerLock(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	agents := filepath.Join(root, ".agents")
	if err := os.Mkdir(agents, 0o700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(agents, "lock.yaml")
	data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo})
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, decode, err := ReadLayerLock(path, path, lockfile.ScopeRepo, nil)
		if err != nil || decode {
			b.Fatalf("read managed lock: decode=%t err=%v", decode, err)
		}
	}
}
