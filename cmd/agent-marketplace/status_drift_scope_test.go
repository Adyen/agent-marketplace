package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// TestStatusScopesDriftSuppressionToIncompleteMarketplacePin pins the scoped
// drift-suppression contract: one drift-incomplete Marketplace pin (healthy
// endpoint, no cached acquisition facts) must suppress only the removal
// candidates attributable to its installed name — reported as an explicit
// user-visible unknown — while unrelated managed orphans stay visible in
// ORPHANED FILES and the drift summary.
func TestStatusScopesDriftSuppressionToIncompleteMarketplacePin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	contentDigest := "sha256:" + strings.Repeat("a", 64)
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
marketplaces:
  - url: `+server.URL+`
    instance_id: mp
    resources:
      - registry_id: src
        kind: skill
        name: review
        revision: v1
        digest: `+contentDigest+`
`)
	withWorkingDir(t, repoRoot)

	// Plant two managed-output orphans (symlinks into the cache, which marks
	// them agent-marketplace-managed): one carrying the incomplete pin's
	// installed name and one unrelated. The marketplace pin was never
	// acquired, so no cached facts can prove either attribution.
	cacheDir := cache.CacheDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "compiled"), 0o755); err != nil {
		t.Fatalf("mkdir cache compiled dir: %v", err)
	}
	skillsDir := filepath.Join(repoRoot, ".factory", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	for _, name := range []string{"review", "stray-orphan"} {
		target := filepath.Join(cacheDir, "compiled", name)
		if err := os.WriteFile(target, []byte("# "+name), 0o644); err != nil {
			t.Fatalf("write orphan target: %v", err)
		}
		if err := os.Symlink(target, filepath.Join(skillsDir, name)); err != nil {
			t.Fatalf("symlink orphan %s: %v", name, err)
		}
	}

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status to stay soft for a drift-incomplete pin: %v\n%s", err, output)
	}
	for _, needle := range []string{
		"ORPHANED FILES",
		"stray-orphan",
		filepath.Join(repoRoot, ".factory", "skills", "stray-orphan"),
		"Marketplace drift is unknown",
		"review",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
	if strings.Contains(output, filepath.Join(repoRoot, ".factory", "skills", "review")) {
		t.Fatalf("status reported the incomplete pin's unattributable output as an orphan, got:\n%s", output)
	}
}
