package tui

import (
	"context"
	"os"
	"path/filepath"
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

// writeCollectionSourceFixture writes a skill and a collection that selects it.
func writeCollectionSourceFixture(t *testing.T, root string) {
	t.Helper()
	for _, file := range []struct{ path, body string }{
		{filepath.Join(root, "skills", "alpha", "SKILL.md"), "# alpha\n"},
		{filepath.Join(root, "collections", "tools", "COLLECTION.yaml"), "name: tools\nresources:\n  - name: alpha\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file.path, []byte(file.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// spec: tui-startup/disabling-installed-collection-removes-selection
func TestDisablingInstalledRegistryCollectionRemovesSelection(t *testing.T) {
	cases := []struct {
		name       string
		sourceName string
		// declare writes the collection fixture and returns the sources block
		// that the project config declares for it, plus the Git URL it declares
		// (empty for directory sources).
		declare func(t *testing.T) (block, gitURL string)
		// assertSource checks the written config still declares the source.
		assertSource func(t *testing.T, configPath, gitURL string)
	}{
		{
			name:       "directory",
			sourceName: "local",
			declare: func(t *testing.T) (string, string) {
				source := t.TempDir()
				writeCollectionSourceFixture(t, source)
				return "    directory: " + source + "\n", ""
			},
			assertSource: func(t *testing.T, configPath, gitURL string) {},
		},
		{
			name:       "git",
			sourceName: "tools",
			declare: func(t *testing.T) (string, string) {
				origin := filepath.Join(t.TempDir(), "origin")
				writeCollectionSourceFixture(t, origin)
				runExactRefreshGit(t, origin, "init", "-b", "main")
				runExactRefreshGit(t, origin, "add", ".")
				runExactRefreshGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "collection")
				url := "file://" + origin
				return "    git: " + url + "\n    ref: main\n", url
			},
			assertSource: func(t *testing.T, configPath, gitURL string) {
				cfg, err := config.LoadConfigFromPath(configPath)
				if err != nil {
					t.Fatalf("reload project config: %v\n%s", err, mustReadFile(t, configPath))
				}
				if len(cfg.Registries) != 1 || cfg.Registries[0].URL != gitURL || cfg.Registries[0].Ref != "main" {
					t.Fatalf("disable changed the Git source declaration, want %s@main:\n%s", gitURL, mustReadFile(t, configPath))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBulkFixture(t)
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			block, gitURL := tc.declare(t)
			project := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  " + tc.sourceName + ":\n" + block +
				"resources:\n  " + tc.sourceName + ":\n    include:\n      - collection/tools\n"
			if err := os.WriteFile(f.projectPath, []byte(project), 0o640); err != nil {
				t.Fatal(err)
			}
			snap := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
			if len(snap.errs) > 0 {
				t.Fatalf("snapshot errors: %v", snap.errs)
			}
			collections := collectionRowsNamed(snap.rows, "tools")
			if len(collections) != 1 || collections[0].IsAvailable || !collections[0].EnabledProject {
				t.Fatalf("selected collection rows = %#v, want one installed project row", collections)
			}
			m := newViewModel(t, snap.rows)
			m.workspace = f.workspace
			m.bulkModal = bulkModalState{sectionPath: "status:enabled", scope: scopeProject, scopeSet: true, direction: bulkDirDisable}
			acts, reasons, err := m.evaluateBulkActions(m.bulkModal)
			if err != nil {
				t.Fatalf("evaluateBulkActions: %v", err)
			}
			if len(acts) != 1 || acts[0].skillName != "tools" || acts[0].kind != ir.KindCollection {
				t.Fatalf("installed collection must be toggleable to disable, acts=%v reasons=%v", acts, reasons)
			}
			msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
			if !ok || msg.err != nil {
				t.Fatalf("disable installed collection failed: %#v err=%v", msg, msg.err)
			}
			reloaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
			after := collectionRowsNamed(reloaded.rows, "tools")
			if len(after) != 1 {
				t.Fatalf("want exactly one tools collection row after disable, got %d:\n%s", len(after), mustReadFile(t, f.projectPath))
			}
			if after[0].EnabledProject || !after[0].IsAvailable {
				t.Fatalf("disabled collection row = available:%v enabledProject:%v, want available and not enabled:\n%s", after[0].IsAvailable, after[0].EnabledProject, mustReadFile(t, f.projectPath))
			}
			tc.assertSource(t, f.projectPath, gitURL)
			assertNoConfigStaging(t)
		})
	}
}

func mustPinArtifact(t *testing.T, entry config.MarketplaceEntry, pin config.MarketplaceResourcePin) resolver.ResolvedArtifact {
	t.Helper()
	artifact, ok := marketplace.PinArtifact(entry, pin)
	if !ok {
		t.Fatalf("pin %q has no artifact", pin.Name)
	}
	return artifact
}
