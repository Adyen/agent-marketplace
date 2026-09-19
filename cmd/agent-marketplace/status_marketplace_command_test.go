package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
marketplaces:
  - url: `+server.URL+`
    instance_id: mp
    alias: team
    resources:
      - registry_id: src
        registry_slug: source-slug
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
		"MARKETPLACE PINS", "ALIAS", "REGISTRY_SLUG", "INSTALLED_NAME", "team", "source-slug", "review-installed", "v1", contentDigest,
		"current", "false", "REPO",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected Marketplace status output to contain %q, got:\n%s", needle, output)
		}
	}
}

// TestStatusMatchesSyncCheckForInstalledMarketplaceResource pins the drift
// parity contract: after a clean sync, status must list the installed
// Marketplace resource as synced output and reach the same in-sync drift
// conclusion as sync --check, instead of reporting the resource's
// materialized files as orphaned removals.
func TestStatusMatchesSyncCheckForInstalledMarketplaceResource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	resource := ir.NewSkill("review", "Review", "Review the code carefully.", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	cache := marketplace.DefaultCache()
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("write revision metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}

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

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}

	statusOutput, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status to succeed: %v\n%s", err, statusOutput)
	}
	for _, needle := range []string{"SYNCED RESOURCES", "review", "MARKETPLACE PINS", "In sync"} {
		if !strings.Contains(statusOutput, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, statusOutput)
		}
	}
	for _, unwanted := range []string{"ORPHANED FILES", "to remove", "to write"} {
		if strings.Contains(statusOutput, unwanted) {
			t.Fatalf("status reported drift after a clean sync (%q), got:\n%s", unwanted, statusOutput)
		}
	}

	checkOutput, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("expected sync --check to agree with status, got: %v\n%s", err, checkOutput)
	}
}

// TestStatusDiagnosesUnavailableMarketplaceEndpoint keeps the endpoint-failure
// diagnostic honest: an unreachable endpoint must remain reported on the pin
// rather than being hidden to reach drift parity.
func TestStatusDiagnosesUnavailableMarketplaceEndpoint(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := server.URL
	server.Close()

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
marketplaces:
  - url: `+endpoint+`
    instance_id: mp
    resources:
      - registry_id: src
        kind: skill
        name: review
        revision: v1
        digest: sha256:`+strings.Repeat("a", 64)+`
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status to report the pin failure without a hard error: %v\n%s", err, output)
	}
	for _, needle := range []string{"MARKETPLACE PINS", "unavailable"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}
