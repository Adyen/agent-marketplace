package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDiscoverCollectionSourceDisk(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("collection-%02d", i)
		dir := filepath.Join(root, "collections", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, collectionFileName), []byte("name: "+name+"\nresources:\n  - name: inert-skill\n"), 0o640); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collections, err := DiscoverCollections(root, "catalog", "registry", "ref", "scope")
		if err != nil || len(collections) != 8 {
			b.Fatalf("collection load: %d %v", len(collections), err)
		}
	}
}
