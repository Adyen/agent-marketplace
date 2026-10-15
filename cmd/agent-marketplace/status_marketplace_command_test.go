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
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
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
apiVersion: agent-marketplace/v4
sources:
  team: {marketplace: `+server.URL+`}
resources:
  team:
    include:
      - source-slug/skill/review: {as: review-installed}
`)
	writeFile(t, repoRoot+"/.agents/lock.yaml", `apiVersion: agent-marketplace-lock/v2
scope: repo
marketplaces:
  - url: `+server.URL+`
    instance_id: mp
    registry_bindings:
      - name: source-slug
        registry_id: src
    resources:
      - registry_name: source-slug
        registry_id: src
        kind: skill
        name: review
        revision: v1
        content_digest: `+contentDigest+`
        archive_digest: sha256:`+strings.Repeat("b", 64)+`
        acquisition: mirror
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}
	for _, needle := range []string{
		"MARKETPLACE PINS", "ALIAS", "REGISTRY_SLUG", "INSTALLED_NAME", "team", "source-slug", "review-installed", "v1", contentDigest,
		"current", "false", "REPO",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected Marketplace status output to contain %q, got:\n%s", needle, output)
		}
	}
}
