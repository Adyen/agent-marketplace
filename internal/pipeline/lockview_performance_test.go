package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
)

func TestExactLockViewConsumesVerifiedCatalogWithoutReingestion(t *testing.T) {
	ec, snapshot := exactLockViewGCFixture(t)
	ctx, view := exactLockViewWithContext(context.Background(), ec, false)
	defer view.release()
	entry := ec.Repo.Config.Registries[0]
	if _, _, err := view.exactSnapshot(entry); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snapshot, ".factory", "skills", "keeper", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: keeper\ndescription: changed after verification\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := catalog.LoadRegistriesWithContext(ctx, ec.Merged, filepath.Dir(filepath.Dir(ec.Repo.Path)), catalog.LoadModeCached, view.catalogOption)
	if len(loaded.Errors) != 0 || len(loaded.Catalogs) != 1 {
		t.Fatalf("load verified catalog: %+v", loaded)
	}
	for _, cat := range loaded.Catalogs {
		if len(cat.Entries) != 1 || cat.Entries[0].Name != "keeper" || cat.Entries[0].Description != "kept" {
			t.Fatalf("inventory did not use the verified definition: %+v", cat.Entries)
		}
	}
	_, next := exactLockViewWithContext(ctx, ec, false)
	defer next.release()
	if _, _, err := next.exactSnapshot(entry); !errors.Is(err, cache.ErrExactGitSnapshotUntrusted) {
		t.Fatalf("next invocation must reject modified snapshot: %v", err)
	}
}
