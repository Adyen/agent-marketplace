package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestUpdateMarketplacePinMutatesOnlyAfterPreparationAndSyncs(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := fixture.pin
	old.InstalledName = "chosen-review"
	configData := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: " + fixture.server.URL + "\n    instance_id: mp\n    resources:\n      - source_id: src\n        source_slug: old-source\n        kind: skill\n        name: review\n        revision: old-revision\n        digest: " + old.Digest + "\n        acquisition: mirror\n        installed_name: chosen-review\n"
	if err := os.WriteFile(fixture.configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Update(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot})
	if err != nil {
		t.Fatalf("update Marketplace pin: %v", err)
	}
	if result.UpdatedMarketplacePins != 1 || result.SkippedMarketplacePins != 0 {
		t.Fatalf("Marketplace result = %#v, want one update", result)
	}
	cfg, err := config.LoadConfigFromPath(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Marketplaces[0].Resources[0]
	if got.Digest != fixture.latestDigest || got.SourceSlug != "renamed-source" || got.InstalledName != "chosen-review" {
		t.Fatalf("updated config pin = %#v", got)
	}
}

func TestPlanMarketplaceUpdatePreparesNewMirrorAndPreservesInstalledName(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	old := fixture.pin
	old.InstalledName = "chosen-review"
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{old}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}

	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan Marketplace update: %v", err)
	}
	if len(plans) != 1 || plans[0].Skipped {
		t.Fatalf("plans = %#v, want one update", plans)
	}
	got := plans[0].NewPin
	if got.Digest != fixture.latestDigest || got.Revision != fixture.latestDigest || got.SourceID != old.SourceID || got.SourceSlug != "renamed-source" {
		t.Fatalf("new pin = %#v, want latest identity and metadata", got)
	}
	if got.InstalledName != "chosen-review" || plans[0].ArchiveDigest != fixture.archiveDigest {
		t.Fatalf("prepared local/archive metadata = %q/%q", got.InstalledName, plans[0].ArchiveDigest)
	}
}

func TestPlanMarketplaceUpdateSkipsUnchangedWithoutArtifactFetch(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.latestDigest = fixture.pin.Digest
	fixture.latest.ContentDigest = store.RevisionDigest(fixture.latestDigest)
	fixture.artifactRequests = 0
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}

	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan Marketplace update: %v", err)
	}
	if len(plans) != 1 || !plans[0].Skipped || plans[0].Reason != "content digest unchanged" {
		t.Fatalf("plans = %#v, want unchanged skip", plans)
	}
	if fixture.artifactRequests != 0 {
		t.Fatalf("artifact requests = %d, want zero", fixture.artifactRequests)
	}
}

func TestPlanMarketplaceUpdateFailureDoesNotMutateConfig(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, true)
	before := []byte("apiVersion: agent-marketplace/v2\nmarketplaces: []\n")
	if err := os.WriteFile(fixture.configPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}
	if _, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec); err == nil {
		t.Fatal("expected preparation failure")
	}
	after, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("planning changed config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMarketplaceUpdateRejectsPolicyRejectionWithoutConsent(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	plan := &marketplace.PreparedInstall{
		Pin: config.MarketplaceResourcePin{Digest: digest},
		Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{
			ContentDigest: digest,
			Findings:      []hccartifact.CanonicalFinding{{Code: "danger", Message: "blocked", PolicyOutcome: "reject"}},
		}},
	}
	if err := validateMarketplaceUpdateArtifact(plan); err == nil || !strings.Contains(err.Error(), "rejected by policy") {
		t.Fatalf("policy rejection error = %v", err)
	}
	plan.Artifact.Manifest.Findings[0].PolicyOutcome = "warn"
	plan.Artifact.Manifest.Capabilities = []string{"command:exec"}
	if err := validateMarketplaceUpdateArtifact(plan); err != nil {
		t.Fatalf("warn/capability update should not require consent: %v", err)
	}
}

func TestMarketplaceUpdateRollbackRestoresConfigAfterSyncFailure(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	old := fixture.pin
	configData := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: " + fixture.server.URL + "\n    instance_id: mp\n    resources:\n      - source_id: src\n        source_slug: old-source\n        kind: skill\n        name: review\n        revision: old-revision\n        digest: " + old.Digest + "\n        acquisition: mirror\n"
	if err := os.WriteFile(fixture.configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, commit, err := config.UpdateMarketplacePinInConfigWithSnapshotAndCommit(fixture.configPath, fixture.server.URL, "mp", old, config.MarketplaceResourcePin{SourceID: old.SourceID, SourceSlug: "renamed-source", Kind: old.Kind, Name: old.Name, Revision: fixture.latestDigest, Digest: fixture.latestDigest, Acquisition: config.MarketplaceAcquisitionMirror})
	if err != nil {
		t.Fatal(err)
	}
	cause := context.Canceled
	if err := rollbackConfigMutations([]configMutation{{path: fixture.configPath, snapshot: snapshot, commit: commit}}, cause); err != cause {
		t.Fatalf("rollback error = %v, want %v", err, cause)
	}
	after, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rollback config = %q, want %q", after, before)
	}
}

func TestPlanMarketplaceUpdateRejectsRevokedLatestWithoutMutation(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.latest.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: fixture.latestDigest, Reason: "withdrawn"}
	before := []byte("apiVersion: agent-marketplace/v2\nmarketplaces: []\n")
	if err := os.WriteFile(fixture.configPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}
	if _, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revocation error = %v", err)
	}
	after, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("revocation planning changed config")
	}
}

func TestUpdateSkipsSyncForOnlyUnsupportedMarketplacePins(t *testing.T) {
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	t.Setenv("HOME", t.TempDir())

	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	configData := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: https://market.example\n    instance_id: mp\n    resources:\n      - source_id: src\n        kind: collection\n        name: bundle\n        revision: v1\n        digest: sha256:" + strings.Repeat("a", 64) + "\n        acquisition: mirror\n      - source_id: index-src\n        kind: skill\n        name: indexed\n        revision: v1\n        digest: sha256:" + strings.Repeat("b", 64) + "\n        acquisition: index-only\n        source_commit: " + strings.Repeat("c", 40) + "\n        source_manifest_digest: sha256:" + strings.Repeat("d", 64) + "\n"
	if err := os.WriteFile(configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.SkippedMarketplacePins != 2 || result.UpdatedMarketplacePins != 0 {
		t.Fatalf("Marketplace result = %#v, want two skipped pins", result)
	}
	reasons := map[string]bool{}
	for _, pin := range result.MarketplacePins {
		reasons[pin.Reason] = true
	}
	for _, reason := range []string{"collection pin updates are not supported", "index-only pin updates are not supported"} {
		if !reasons[reason] {
			t.Fatalf("skip reasons = %#v, missing %q", reasons, reason)
		}
	}
}

func TestUpdateSkipsUnchangedMarketplacePinWithoutTargetSet(t *testing.T) {
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	t.Setenv("HOME", t.TempDir())
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.latestDigest = fixture.pin.Digest
	fixture.latest.ContentDigest = store.RevisionDigest(fixture.latestDigest)
	configData := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: " + fixture.server.URL + "\n    instance_id: mp\n    resources:\n      - source_id: src\n        kind: skill\n        name: review\n        revision: old-revision\n        digest: " + fixture.pin.Digest + "\n        acquisition: mirror\n"
	if err := os.WriteFile(fixture.configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Update(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot})
	if err != nil {
		t.Fatalf("Update unchanged Marketplace pin: %v", err)
	}
	if result.SkippedMarketplacePins != 1 || result.UpdatedMarketplacePins != 0 {
		t.Fatalf("Marketplace result = %#v, want one unchanged skip", result)
	}
	if result.MarketplacePins[0].Reason != "content digest unchanged" {
		t.Fatalf("skip reason = %q", result.MarketplacePins[0].Reason)
	}
}

func TestUpdateRequiresTargetBeforePreparingChangedMarketplacePin(t *testing.T) {
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	t.Setenv("HOME", t.TempDir())
	fixture := newMarketplaceUpdateFixture(t, false)
	configData := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: " + fixture.server.URL + "\n    instance_id: mp\n    resources:\n      - source_id: src\n        kind: skill\n        name: review\n        revision: old-revision\n        digest: " + fixture.pin.Digest + "\n        acquisition: mirror\n"
	if err := os.WriteFile(fixture.configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Update(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot})
	var missing *TargetHarnessSetMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("Update error = %v, want TargetHarnessSetMissingError", err)
	}
	if fixture.artifactRequests != 0 {
		t.Fatalf("artifact requests = %d, want zero before target validation", fixture.artifactRequests)
	}
}

func TestPlanMarketplaceUpdateSkipsOrganizationPinAsRequired(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.Organization, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}
	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan organization pin: %v", err)
	}
	if len(plans) != 1 || !plans[0].Skipped || !plans[0].Required || plans[0].Reason == "" {
		t.Fatalf("organization plan = %#v, want required skip", plans)
	}
}

type marketplaceUpdateFixture struct {
	server           *httptest.Server
	repoRoot         string
	configPath       string
	pin              config.MarketplaceResourcePin
	latest           store.ResourceRevision
	latestDigest     string
	archiveDigest    string
	artifact         []byte
	artifactRequests int
}

func newMarketplaceUpdateFixture(t *testing.T, failArtifact bool) *marketplaceUpdateFixture {
	t.Helper()
	digestOld := "sha256:" + strings.Repeat("a", 64)
	skill := ir.NewSkill("review", "Review", "Review carefully "+t.TempDir(), []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.SourceID = "src"
	canonical.Manifest.SourceSlug = "renamed-source"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	latestDigest := canonical.Manifest.ContentDigest
	fixture := &marketplaceUpdateFixture{
		repoRoot:     filepath.Join(t.TempDir(), "repo"),
		pin:          config.MarketplaceResourcePin{SourceID: "src", SourceSlug: "old-source", Kind: "skill", Name: "review", Revision: "old-revision", Digest: digestOld, Acquisition: config.MarketplaceAcquisitionMirror},
		latestDigest: latestDigest, archiveDigest: archiveDigest, artifact: raw,
	}
	fixture.latest = store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(latestDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	})
	mux.HandleFunc("/api/v1/sources/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.SourceDTO{ID: "src", Slug: "renamed-source", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/renamed-source/skill/review", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(fixture.latest) })
	mux.HandleFunc("/api/v1/resources/src/skill/review/revisions/", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(fixture.latest) })
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		fixture.artifactRequests++
		if failArtifact {
			http.Error(w, "prefetch failed", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(fixture.artifact)
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	fixture.configPath = filepath.Join(fixture.repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}
