package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestStatusReportsMarketplacePinsWithoutDroppingThem(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
		})
	}))
	defer server.Close()

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
marketplaces:
  - url: `+server.URL+`
    instance_id: mp
    alias: team
    resources:
      - source_id: src
        source_slug: source-slug
        kind: skill
        name: review
        revision: v1
        digest: `+contentDigest+`
        installed_name: review-installed
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}
	for _, needle := range []string{
		"MARKETPLACE PINS", "ALIAS", "SOURCE_SLUG", "INSTALLED_NAME", "team", "source-slug", "review-installed", "v1", contentDigest,
		"current", "false", "REPO",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected Marketplace status output to contain %q, got:\n%s", needle, output)
		}
	}
}
