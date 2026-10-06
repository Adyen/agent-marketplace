package catalog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadPrimarySourceDisk(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	root := b.TempDir()
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("source-%d", i)
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(fmt.Sprintf("---\nname: %s\ndescription: source control\n---\n\ninert body\n", name)), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cat, err := Load(root, "claude")
		if err != nil || len(cat.Entries) != 8 {
			b.Fatalf("primary disk load positive control failed: %+v %v", cat, err)
		}
	}
}
