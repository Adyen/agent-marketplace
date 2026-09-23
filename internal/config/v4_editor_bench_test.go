package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkEditV4ResourceSelectionDirectoryDiskIO(b *testing.B) {
	root := b.TempDir()
	source := filepath.Join(root, "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		b.Fatal(err)
	}
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		action := V4SelectionEnable
		if i%2 == 1 {
			action = V4SelectionDisable
		}
		handled, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", action)
		if !handled || err != nil {
			b.Fatalf("edit %d handled=%v err=%v", i, handled, err)
		}
	}
}
