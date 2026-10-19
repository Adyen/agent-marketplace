package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func installedCollectionFixtures() (*config.Config, map[string]catalogInfo, resolver.ResolvedArtifact, config.MarketplaceEntry, config.MarketplaceResourcePin) {
	digest := "sha256:" + strings.Repeat("c", 64)
	gitCollection := ir.NewCollection("tools", "", "", []ir.CollectionMember{{Name: "alpha", Kind: ir.KindSkill}}, nil, nil, ir.SourceRef{})
	gitCfg := &config.Config{Registries: []config.RegistryEntry{{
		URL: "git@example.com:repo.git", Ref: "main", CatalogKey: "git-key", Source: config.RepoCommitted,
		Collections: config.KindSelector{Include: []string{"tools"}},
	}}}
	catalogs := map[string]catalogInfo{"git-key": {Entries: []catalog.Entry{
		{Kind: ir.KindCollection, Name: "tools", Resource: gitCollection, Path: "/cache/repo/collections/tools"},
		{Kind: ir.KindSkill, Name: "alpha", Path: "/cache/repo/alpha"},
	}}}
	gitRoot := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: ir.KindCollection, Name: "tools", Resource: gitCollection},
		RegistryURL: "git@example.com:repo.git", Ref: "main", Source: config.RepoCommitted,
		CatalogKey: "git-key",
	}
	entry := config.MarketplaceEntry{
		URL: "https://market.example", InstanceID: "market-id", Source: config.UserPersonal,
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "collections", RegistrySlug: "collections", Kind: "collection", Name: "bundle", Revision: digest, Digest: digest,
			Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
		}},
	}
	return gitCfg, catalogs, gitRoot, entry, entry.Resources[0]
}

func collectionRowsNamed(rows []row, name string) []row {
	var out []row
	for _, r := range rows {
		if r.Artifact.Kind == ir.KindCollection && r.Artifact.Name == name {
			out = append(out, r)
		}
	}
	return out
}

// spec: tui-startup/verified-selected-collection-row-is-installed
func TestVerifiedRowsRenderSelectedCollectionsAsInstalled(t *testing.T) {
	gitCfg, catalogs, gitRoot, entry, pin := installedCollectionFixtures()
	mpArtifact, ok := marketplace.PinArtifact(entry, pin)
	if !ok {
		t.Fatal("collection pin must map to an artifact")
	}
	cfg := &config.Config{Registries: gitCfg.Registries, Marketplaces: []config.MarketplaceEntry{entry}}

	rows := buildRows(cfg, catalogs, []resolver.ResolvedArtifact{gitRoot, mpArtifact}, nil, materialize.DriftReport{})

	gitRows := collectionRowsNamed(rows, "tools")
	if len(gitRows) != 1 || gitRows[0].IsAvailable || !gitRows[0].EnabledProject {
		t.Fatalf("selected Git collection rows = %#v, want one installed project row", gitRows)
	}
	mpRows := collectionRowsNamed(rows, "bundle")
	if len(mpRows) != 1 || mpRows[0].IsAvailable || !mpRows[0].EnabledGlobal {
		t.Fatalf("selected Marketplace collection rows = %#v, want one installed global row", mpRows)
	}
	if mpRows[0].MarketplaceCollectionOnly {
		t.Fatal("a direct Marketplace collection pin must not be collection-only")
	}
}

// spec: tui-startup/provisional-selected-collection-row-is-installed
func TestProvisionalRowsRenderSelectedCollectionsAsInstalled(t *testing.T) {
	gitCfg, _, _, entry, pin := installedCollectionFixtures()
	nested := pin
	nested.Name = "nested-bundle"
	nested.Direct = false
	nested.SelectedBy = config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: pin.Revision}})
	entry.Resources = append(entry.Resources, nested)
	cfg := &config.Config{Registries: gitCfg.Registries, Marketplaces: []config.MarketplaceEntry{entry}}

	rows := buildProvisionalRows(cfg, nil, nil)

	gitRows := collectionRowsNamed(rows, "tools")
	if len(gitRows) != 1 || gitRows[0].IsAvailable || !gitRows[0].EnabledProject || !gitRows[0].PendingVerification {
		t.Fatalf("provisional Git collection rows = %#v, want one pending installed project row", gitRows)
	}
	mpRows := collectionRowsNamed(rows, "bundle")
	if len(mpRows) != 1 || mpRows[0].IsAvailable || !mpRows[0].EnabledGlobal || !mpRows[0].PendingVerification {
		t.Fatalf("provisional Marketplace collection rows = %#v, want one pending installed global row", mpRows)
	}
	if got := collectionRowsNamed(rows, "nested-bundle"); len(got) != 0 {
		t.Fatalf("non-direct nested collection pin must stay out of provisional rows, got %#v", got)
	}
}

// spec: tui-startup/disabling-installed-collection-removes-selection
func TestDisablingInstalledMarketplaceCollectionRemovesSelection(t *testing.T) {
	f := newBulkFixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	entry := config.MarketplaceEntry{
		URL: "https://market.example", InstanceID: "market-id", Source: config.RepoCommitted,
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "collections", RegistrySlug: "collections", Kind: "collection", Name: "bundle", Revision: digest, Digest: digest,
			Acquisition: config.MarketplaceAcquisitionMirror, Direct: true,
		}},
	}
	project := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n" +
		"sources: {mkt: {marketplace: https://market.example}}\n" +
		"resources:\n  mkt:\n    include:\n" +
		"      - collections/collection/bundle\n"
	if err := os.WriteFile(f.projectPath, []byte(project), 0o640); err != nil {
		t.Fatal(err)
	}
	rows := buildRows(&config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, []resolver.ResolvedArtifact{mustPinArtifact(t, entry, entry.Resources[0])}, nil, materialize.DriftReport{})
	m := newViewModel(t, rows)
	m.workspace = f.workspace
	m.bulkModal = bulkModalState{sectionPath: "status:enabled", scope: scopeProject, scopeSet: true, direction: bulkDirDisable}
	acts, reasons, err := m.evaluateBulkActions(m.bulkModal)
	if err != nil {
		t.Fatalf("evaluateBulkActions: %v", err)
	}
	if len(acts) != 1 || acts[0].skillName != "bundle" || acts[0].kind != ir.KindCollection {
		t.Fatalf("installed collection must be toggleable to disable, acts=%v reasons=%v", acts, reasons)
	}
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("disable installed collection failed: %#v err=%v", msg, msg.err)
	}
	if updated := string(mustReadFile(t, f.projectPath)); strings.Contains(updated, "collections/collection/bundle") {
		t.Fatalf("collection selection must be removed:\n%s", updated)
	}
	assertNoConfigStaging(t)
}

func mustPinArtifact(t *testing.T, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) resolver.ResolvedArtifact {
	t.Helper()
	artifact, ok := marketplace.PinArtifact(entry, pin)
	if !ok {
		t.Fatalf("pin %q has no artifact", pin.Name)
	}
	return artifact
}
