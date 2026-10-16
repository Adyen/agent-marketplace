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
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func testMarketplaceUpdateReconcile(_ context.Context, _, _ string, _ []RefBump, edits []StagedConfigEdit, lockEdits []StagedLockEdit) (DependencyLockView, error) {
	for _, edit := range edits {
		if err := os.MkdirAll(filepath.Dir(edit.Path), 0o755); err != nil {
			return DependencyLockView{}, err
		}
		if err := os.WriteFile(edit.Path, edit.Bytes, edit.Mode); err != nil {
			return DependencyLockView{}, err
		}
	}
	for _, edit := range lockEdits {
		if err := os.MkdirAll(filepath.Dir(edit.Path), 0o755); err != nil {
			return DependencyLockView{}, err
		}
		if err := os.WriteFile(edit.Path, edit.Bytes, edit.Mode); err != nil {
			return DependencyLockView{}, err
		}
	}
	return DependencyLockView{}, nil
}

func TestUpdateMarketplacePinMutatesOnlyAfterPreparationAndSyncs(t *testing.T) {
	home := isolateConsumerPipeline(t)
	fixture := newMarketplaceUpdateFixture(t, false)
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.writeV4Selection(t, "old-source/skill/review: {as: chosen-review}")
	configBefore, configInfo := layerFreezeFile(t, fixture.configPath)
	lockPath := filepath.Join(filepath.Dir(fixture.configPath), "lock.yaml")
	lockBefore, _ := layerFreezeFile(t, lockPath)
	result, err := Update(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot, DependencyReconcile: testMarketplaceUpdateReconcile})
	if err != nil {
		t.Fatalf("update Marketplace pin: %v", err)
	}
	if result.UpdatedMarketplacePins != 1 || result.SkippedMarketplacePins != 0 {
		t.Fatalf("Marketplace result = %#v, want one update", result)
	}
	layerAssertFileUnchanged(t, fixture.configPath, configBefore, configInfo)
	doc := layerMarketplaceLockDoc(t, lockPath)
	record := marketplaceLockResource(doc, "skill", "review")
	if record == nil || string(record.ContentDigest) != fixture.latestDigest || record.RegistryID != "src" || string(record.ArchiveDigest) != fixture.archiveDigest {
		t.Fatalf("updated lock evidence = %#v", record)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) == string(lockBefore) {
		t.Fatalf("lock evidence did not change: %v", err)
	}
	loaded := LoadEffective(context.Background(), ConfigLoadStrict, config.LoadEffectiveOptions{RepoRoot: fixture.repoRoot}, nil)
	if len(loaded.Errors) != 0 || len(loaded.Merged.Marketplaces) != 1 || len(loaded.Merged.Marketplaces[0].Resources) != 1 {
		t.Fatalf("updated runtime projection = %#v, errors = %v", loaded.Merged, loaded.Errors)
	}
	got := loaded.Merged.Marketplaces[0].Resources[0]
	if got.Digest != fixture.latestDigest || got.InstalledName != "chosen-review" {
		t.Fatalf("updated runtime pin = %#v", got)
	}
	if fixture.artifactRequests == 0 {
		t.Fatal("changed mirror update did not prepare its artifact")
	}
	if _, err := os.Stat(filepath.Join(fixture.repoRoot, ".factory", "skills", "chosen-review", "SKILL.md")); err != nil {
		t.Fatalf("updated installed-name output missing: %v", err)
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
	if got.Digest != fixture.latestDigest || got.Revision != fixture.latestDigest || got.RegistryID != old.RegistryID || got.RegistrySlug != "renamed-source" {
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
	fixture.latest.ArchiveDigest = store.ArchiveDigest(fixture.priorArchiveDigest)
	fixture.latest = testAssessedResourceRevision(t, fixture.latest.ResourceRevision, store.AssessmentDecisionAllow, nil)
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

func TestMarketplaceOccurrenceComparisonPreservesUpdatePlanningContract(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	pin := config.MarketplaceResourcePin{
		Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
	}
	intrinsic := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)),
	}
	occurrence := func(commit, manifestDigest string) store.AssessedResourceRevision {
		revision := testAssessedResourceRevision(t, intrinsic, store.AssessmentDecisionAllow, nil)
		revision.Occurrence.Commit = commit
		revision.Occurrence.ManifestDigest = store.RevisionDigest(manifestDigest)
		return revision
	}
	cases := []struct {
		name    string
		pin     config.MarketplaceResourcePin
		latest  store.AssessedResourceRevision
		changed bool
	}{
		{
			name: "same occurrence", pin: pin,
			latest: occurrence(commit, manifestDigest),
		},
		{
			name: "different commit", pin: pin,
			latest:  occurrence(strings.Repeat("c", 40), manifestDigest),
			changed: true,
		},
		{
			name: "different manifest", pin: pin,
			latest:  occurrence(commit, "sha256:"+strings.Repeat("d", 64)),
			changed: true,
		},
		{
			name: "missing occurrence", pin: pin,
			latest:  occurrence("", ""),
			changed: true,
		},
		{
			name: "mirror ignores occurrence", pin: config.MarketplaceResourcePin{Acquisition: config.MarketplaceAcquisitionMirror},
			latest: occurrence(strings.Repeat("c", 40), "sha256:"+strings.Repeat("d", 64)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketplace.IndexOnlyOccurrenceChanged(tc.pin, tc.latest); got != tc.changed {
				t.Fatalf("occurrence changed = %t, want %t", got, tc.changed)
			}
		})
	}
}

func TestPlanMarketplaceUpdatePreparesSameContentForChangedIndexOccurrence(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	pin := fixture.pin
	pin.Digest = fixture.latestDigest
	pin.Acquisition = config.MarketplaceAcquisitionIndexOnly
	pin.RegistryCommit = strings.Repeat("a", 40)
	pin.RegistryManifestDigest = "sha256:" + strings.Repeat("b", 64)
	fixture.latest.Occurrence.Commit = strings.Repeat("c", 40)
	fixture.latest.Occurrence.ManifestDigest = store.RevisionDigest(pin.RegistryManifestDigest)
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}

	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan Marketplace update: %v", err)
	}
	if len(plans) != 1 || plans[0].Skipped || plans[0].NewPin.Digest != pin.Digest {
		t.Fatalf("plans = %#v, want prepared same-content update", plans)
	}
	if fixture.artifactRequests != 1 {
		t.Fatalf("artifact requests = %d, want one prepared update", fixture.artifactRequests)
	}
}

func TestPlanMarketplaceUpdateFailureDoesNotMutateConfig(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, true)
	before := []byte("apiVersion: agent-marketplace/v4\n")
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
	if fixture.artifactRequests == 0 {
		t.Fatal("preparation failed before the failing artifact endpoint was exercised")
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
		Pin:      config.MarketplaceResourcePin{Digest: digest},
		Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{ContentDigest: digest}},
		Revision: store.AssessedResourceRevision{Assessment: store.ResourceAssessment{Decision: store.AssessmentDecisionReject}},
	}
	if err := validateMarketplaceUpdateArtifact(plan); err == nil || !strings.Contains(err.Error(), "rejected by policy") {
		t.Fatalf("policy rejection error = %v", err)
	}
	plan.Revision.Assessment.Decision = store.AssessmentDecisionWarn
	plan.Artifact.Manifest.Capabilities = []string{"command:exec"}
	if err := validateMarketplaceUpdateArtifact(plan); err != nil {
		t.Fatalf("warn/capability update should not require consent: %v", err)
	}
}

func TestMarketplaceUpdateRollbackRestoresConfigAfterSyncFailure(t *testing.T) {
	home := isolateConsumerPipeline(t)
	repo := t.TempDir()
	path := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	installV4MarketplaceLayer(t, repo, path, entry, layerPreparePins(t, fake, "skill", "review"))
	before, beforeInfo := layerFreezeFile(t, path)
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)
	fake.setSkill(t, registry, "review", "new review content")
	wantErr := errors.New("sync reconciliation failed")
	called := false
	_, err := Update(context.Background(), UpdateOptions{RepoRoot: repo, DependencyReconcile: func(_ context.Context, _, _ string, _ []RefBump, _ []StagedConfigEdit, edits []StagedLockEdit) (DependencyLockView, error) {
		called = true
		if len(edits) != 1 || string(edits[0].Bytes) == string(lockBefore) {
			t.Fatal("control did not stage updated lock evidence")
		}
		return DependencyLockView{}, wantErr
	}})
	if !called || !errors.Is(err, wantErr) {
		t.Fatalf("update error = %v, called=%t", err, called)
	}
	layerAssertFileUnchanged(t, path, before, beforeInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
}

func TestPlanMarketplaceUpdateRejectsRevokedLatestWithoutMutation(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.latest.Revocation = &store.Revocation{Kind: store.RevocationRegistry, TargetID: fixture.pin.RegistryID, Reason: "withdrawn"}
	before := []byte("apiVersion: agent-marketplace/v4\n")
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
	isolateConsumerPipeline(t)

	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	configData := "apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [source/collection/bundle, index-source/skill/indexed]\n"
	writeFile(t, configPath, configData)
	collectionDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "source", RegistryID: "src"}, {Name: "index-source", RegistryID: "index-src"}},
			Resources: []lockfile.MarketplaceResourceLock{
				{RegistryName: "source", RegistryID: "src", Kind: "collection", Name: "bundle", Revision: "v1", ContentDigest: collectionDigest, CollectionManifestDigest: collectionDigest, Acquisition: "mirror"},
				{RegistryName: "index-source", RegistryID: "index-src", Kind: "skill", Name: "indexed", Revision: "v1", ContentDigest: lockfile.Digest("sha256:" + strings.Repeat("b", 64)), ArchiveDigest: lockfile.Digest("sha256:" + strings.Repeat("e", 64)), Acquisition: "index-only", RegistryCommit: strings.Repeat("c", 40), RegistryManifestDigest: lockfile.Digest("sha256:" + strings.Repeat("d", 64)), SourcePath: "skills/indexed"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	writeFile(t, lockPath, string(lockData))
	configBefore, configInfo := layerFreezeFile(t, configPath)
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)

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
	layerAssertFileUnchanged(t, configPath, configBefore, configInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("unsupported-only update created harness output: %v", err)
	}
}

func TestUpdateSkipsUnchangedMarketplacePinWithoutTargetSet(t *testing.T) {
	isolateConsumerPipeline(t)
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.latestDigest = fixture.pin.Digest
	fixture.latest.ContentDigest = store.RevisionDigest(fixture.latestDigest)
	fixture.latest.ArchiveDigest = store.ArchiveDigest(fixture.priorArchiveDigest)
	fixture.latest = testAssessedResourceRevision(t, fixture.latest.ResourceRevision, store.AssessmentDecisionAllow, nil)
	fixture.writeV4Selection(t, "old-source/skill/review")
	configBefore, configInfo := layerFreezeFile(t, fixture.configPath)
	lockPath := filepath.Join(filepath.Dir(fixture.configPath), "lock.yaml")
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)

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
	if fixture.artifactRequests != 0 {
		t.Fatalf("unchanged update fetched %d artifacts", fixture.artifactRequests)
	}
	layerAssertFileUnchanged(t, fixture.configPath, configBefore, configInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
}

func TestUpdateRequiresTargetBeforePreparingChangedMarketplacePin(t *testing.T) {
	isolateConsumerPipeline(t)
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.writeV4Selection(t, "old-source/skill/review")
	configBefore, configInfo := layerFreezeFile(t, fixture.configPath)
	lockPath := filepath.Join(filepath.Dir(fixture.configPath), "lock.yaml")
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)

	_, err := Update(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot})
	var missing *TargetHarnessSetMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("Update error = %v, want TargetHarnessSetMissingError", err)
	}
	if fixture.artifactRequests != 0 {
		t.Fatalf("artifact requests = %d, want zero before target validation", fixture.artifactRequests)
	}
	layerAssertFileUnchanged(t, fixture.configPath, configBefore, configInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
}

func TestPlanMarketplaceUpdateSkipsExternallyManagedOrganizationPin(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	for _, required := range []bool{false, true} {
		entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.Organization, Required: required, Resources: []config.MarketplaceResourcePin{fixture.pin}}
		ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}
		plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
		if err != nil {
			t.Fatalf("plan organization pin: %v", err)
		}
		if len(plans) != 1 || !plans[0].Skipped || plans[0].Required != required || plans[0].Reason == "" {
			t.Fatalf("organization plan = %#v, want skipped with required=%t", plans, required)
		}
	}
}

type marketplaceUpdateFixture struct {
	server             *httptest.Server
	repoRoot           string
	configPath         string
	pin                config.MarketplaceResourcePin
	latest             store.AssessedResourceRevision
	latestDigest       string
	archiveDigest      string
	priorArchiveDigest string
	priorArtifact      []byte
	artifact           []byte
	artifactRequests   int
	latestHook         func()
}

// writeV4Selection seeds the repository-owned request and its prior exact lock.
// The request deliberately has no revision constraint, so update can advance it.
func (f *marketplaceUpdateFixture) writeV4Selection(t *testing.T, request string) {
	t.Helper()
	prior := testAssessedResourceRevision(t, store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(f.pin.Digest), ArchiveDigest: store.ArchiveDigest(f.priorArchiveDigest)}, store.AssessmentDecisionAllow, nil)
	cache := marketplace.DefaultCache()
	if _, err := cache.WriteResourceRevision(f.server.URL, "src", "skill", "review", f.pin.Digest, prior); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(f.priorArchiveDigest, f.priorArtifact); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(f.priorArchiveDigest); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.configPath, "apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: "+f.server.URL+"}\nresources:\n  public:\n    - "+request+"\n")
	data, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{URL: f.server.URL, InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "src"}},
			Resources:        []lockfile.MarketplaceResourceLock{{RegistryName: "old-source", RegistryID: "src", Kind: "skill", Name: "review", Revision: f.pin.Revision, ContentDigest: lockfile.Digest(f.pin.Digest), ArchiveDigest: lockfile.Digest(f.priorArchiveDigest), Acquisition: "mirror"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(f.configPath), "lock.yaml"), string(data))
}

func newMarketplaceUpdateFixture(t *testing.T, failArtifact bool) *marketplaceUpdateFixture {
	t.Helper()
	prior, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "Previously reviewed content", []string{"user"}, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	prior.Manifest.RegistryID = "src"
	prior.Manifest.RegistrySlug = "old-source"
	priorRaw, priorArchiveDigest, err := hccartifact.Encode(prior)
	if err != nil {
		t.Fatal(err)
	}
	digestOld := prior.Manifest.ContentDigest
	skill := ir.NewSkill("review", "Review", "Review carefully "+t.TempDir(), []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "renamed-source"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	latestDigest := canonical.Manifest.ContentDigest
	fixture := &marketplaceUpdateFixture{
		repoRoot:     filepath.Join(t.TempDir(), "repo"),
		pin:          config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "old-source", Kind: "skill", Name: "review", Revision: "old-revision", Digest: digestOld, Acquisition: config.MarketplaceAcquisitionMirror},
		latestDigest: latestDigest, archiveDigest: archiveDigest, priorArchiveDigest: priorArchiveDigest, priorArtifact: priorRaw, artifact: raw,
	}
	fixture.latest = testAssessedResourceRevision(t, store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(latestDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)}, store.AssessmentDecisionAllow, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{ID: "src", Slug: "renamed-source", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	serveLatest := func(w http.ResponseWriter, r *http.Request) {
		if fixture.latestHook != nil {
			fixture.latestHook()
		}
		_ = json.NewEncoder(w).Encode(fixture.latest)
	}
	mux.HandleFunc("/api/v1/resources/renamed-source/skill/review", serveLatest)
	mux.HandleFunc("/api/v1/resources/src/skill/review/revisions/", serveLatest)
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

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestPlanMarketplaceUpdateRefreshesDriftedArchiveEvidence(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.pin.Digest = fixture.latestDigest
	fixture.pin.LockArchiveDigest = "sha256:" + strings.Repeat("0", 64)
	fixture.artifactRequests = 0
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}

	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan Marketplace update: %v", err)
	}
	if len(plans) != 1 || plans[0].Skipped || plans[0].ArchiveDigest != fixture.archiveDigest {
		t.Fatalf("plans = %#v, want evidence-refresh update to %q", plans, fixture.archiveDigest)
	}
	if fixture.artifactRequests != 1 {
		t.Fatalf("artifact requests = %d, want one verification fetch", fixture.artifactRequests)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestPlanMarketplaceUpdateStagesVerifiedDigestNotAdvertised(t *testing.T) {
	fixture := newMarketplaceUpdateFixture(t, false)
	fixture.pin.Digest = fixture.latestDigest
	fixture.pin.LockArchiveDigest = "sha256:" + strings.Repeat("0", 64)
	fixture.artifactRequests = 0
	var metadataReads atomic.Int64
	stale := fixture.latest
	stale.ArchiveDigest = store.ArchiveDigest("sha256:" + strings.Repeat("9", 64))
	fixture.latest = stale
	fixture.latestHook = func() {
		if metadataReads.Add(1) > 1 {
			fixture.latest.ArchiveDigest = store.ArchiveDigest(fixture.archiveDigest)
		}
	}
	entry := config.MarketplaceEntry{URL: fixture.server.URL, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{fixture.pin}}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, TargetHarnesses: []string{"factory"}}

	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: fixture.repoRoot}, ec)
	if err != nil {
		t.Fatalf("plan Marketplace update: %v", err)
	}
	if len(plans) != 1 || plans[0].Skipped || plans[0].ArchiveDigest != fixture.archiveDigest {
		t.Fatalf("plans = %#v, want verified digest %q", plans, fixture.archiveDigest)
	}
}
