package harness

import (
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func BenchmarkResolveDevinRepoRoot(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	repo := b.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".devin"), 0755); err != nil {
		b.Fatal(err)
	}
	if _, err := ResolveDestinations(hcc.TargetDevin, false, repo); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ResolveDestinations(hcc.TargetDevin, false, repo); err != nil {
			b.Fatal(err)
		}
	}
}
