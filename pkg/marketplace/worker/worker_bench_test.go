package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkRefresh(b *testing.B) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(b)
	defer cleanup()

	cfg := createTestConfig(b, repoDir)
	meta, blobs := openTestStores(b)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		b.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Source{ID: store.SourceID(cfg.Sources[0].ID)}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Make each iteration unique by touching a file so the commit changes.
		marker := fmt.Sprintf("bench-%d", i)
		if err := os.WriteFile(repoDir+"/"+marker, []byte("x"), 0o644); err != nil {
			b.Fatalf("write marker: %v", err)
		}
		cmd := exec.Command("git", "add", marker)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git add: %v\n%s", err, out)
		}
		cmd = exec.Command("git", "commit", "-q", "-m", marker, "--allow-empty")
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git commit: %v\n%s", err, out)
		}

		if err := Refresh(ctx, deps, src); err != nil {
			b.Fatalf("Refresh: %v", err)
		}
	}
}
