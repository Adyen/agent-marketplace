package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func tuiLockDoc(t *testing.T, path string) lockfile.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func tuiAssessedResourceRevision(t *testing.T, revision store.ResourceRevision) store.AssessedResourceRevision {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               revision.Coordinate,
		ContentDigest:            revision.ContentDigest,
		AnalyzerVersion:          "tui-test",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize assessment: %v", err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate:     revision.Coordinate,
			ContentDigest:  revision.ContentDigest,
			Commit:         strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
}

func warmMarketplaceSkillPin(t *testing.T, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) string {
	t.Helper()
	resource := ir.NewSkill(pin.Name, "Review code", "Review the code carefully.", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	pin.Digest = canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = pin.RegistryID
	canonical.Manifest.Revision = pin.Digest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	cache := marketplace.DefaultCache()
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	revision := tuiAssessedResourceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(entry.InstanceID), Registry: store.RegistryID(pin.RegistryID), Kind: pin.Kind, Name: pin.Name},
		ContentDigest: store.RevisionDigest(pin.Digest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	if _, err := cache.WriteResourceRevision(entry.URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, revision); err != nil {
		t.Fatal(err)
	}
	return pin.Digest
}

func TestMarketplaceToggleV4EnableDisableReenable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	repoRoot := t.TempDir()
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+entry.URL+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: "source", RegistrySlug: "source", Kind: "skill", Name: "review", Acquisition: config.MarketplaceAcquisitionMirror, Direct: true}
	pin.Digest = warmMarketplaceSkillPin(t, entry, pin)
	pin.Revision = pin.Digest
	entry.Resources = []config.MarketplaceResourcePin{pin}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")

	r := row{
		Artifact:      resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, IsMarketplace: true},
		EnabledGlobal: false,
		RegistryURL:   entry.URL,
		MarketplaceSelections: map[config.Source]marketplaceSelection{
			config.UserPersonal: {Entry: entry, Pin: pin},
		},
	}

	msg := runScopedToggle(repoRoot, scopedToggleAction(r, scopeGlobal))()
	if written, ok := msg.(configWrittenMsg); !ok || written.err != nil {
		t.Fatalf("enable = %#v", msg)
	}
	data, err := os.ReadFile(userPath)
	if err != nil || !strings.Contains(string(data), "source/skill/review") {
		t.Fatalf("enable config = %s, %v", data, err)
	}
	doc := tuiLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("enable lock = %#v", doc)
	}

	r.EnabledGlobal = true
	msg = runScopedToggle(repoRoot, scopedToggleAction(r, scopeGlobal))()
	if written, ok := msg.(configWrittenMsg); !ok || written.err != nil {
		t.Fatalf("disable = %#v", msg)
	}
	data, err = os.ReadFile(userPath)
	if err != nil || strings.Contains(string(data), "source/skill/review") || !strings.Contains(string(data), "marketplace:") {
		t.Fatalf("disable config = %s, %v", data, err)
	}
	doc = tuiLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("disable lock = %#v", doc)
	}

	r.EnabledGlobal = false
	msg = runScopedToggle(repoRoot, scopedToggleAction(r, scopeGlobal))()
	if written, ok := msg.(configWrittenMsg); !ok || written.err != nil {
		t.Fatalf("re-enable = %#v", msg)
	}
	doc = tuiLockDoc(t, lockPath)
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("re-enable lock = %#v", doc)
	}
}

func TestMarketplaceToggleV4EnableMissingEvidenceFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	repoRoot := t.TempDir()
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+entry.URL+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(userPath)
	digest := "sha256:" + strings.Repeat("e", 64)
	pin := config.MarketplaceResourcePin{RegistryID: "source", RegistrySlug: "source", Kind: "skill", Name: "review", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror, Direct: true}
	r := row{
		Artifact:    resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}, IsMarketplace: true},
		RegistryURL: entry.URL,
		MarketplaceSelections: map[config.Source]marketplaceSelection{
			config.UserPersonal: {Entry: entry, Pin: pin},
		},
	}
	msg := runScopedToggle(repoRoot, scopedToggleAction(r, scopeGlobal))()
	written, ok := msg.(configWrittenMsg)
	if !ok || written.err == nil {
		t.Fatalf("cold-cache enable = %#v", msg)
	}
	if after, _ := os.ReadFile(userPath); string(after) != string(before) {
		t.Fatal("failed enable changed config")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("failed enable created lock: %v", err)
	}
}
